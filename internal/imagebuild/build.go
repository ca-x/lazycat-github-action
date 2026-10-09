// Package imagebuild adapts local Docker Buildx OCI exports to toolkit image artifacts.
package imagebuild

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/ca-x/lazycat-github-action/internal/platform"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/types"
	toolkitbuild "github.com/lib-x/lzc-toolkit-go/build"
	"github.com/lib-x/lzc-toolkit-go/oci"
	"go.yaml.in/yaml/v3"
)

type Command struct {
	Args []string
	Dir  string
	Env  []string
}
type Builder struct {
	Target         platform.Target
	Environment    []string
	Stdout, Stderr io.Writer
	Logger         *slog.Logger
	// Run is injectable for tests; by default Docker is invoked without a shell.
	Run func(context.Context, Command) error
}

type imageConfig struct {
	Dockerfile    string  `yaml:"dockerfile"`
	Content       string  `yaml:"dockerfile-content"`
	Context       string  `yaml:"context"`
	Builder       string  `yaml:"builder"`
	UpstreamMatch *string `yaml:"upstream-match"`
}

var aliasPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type artifact struct{ directory string }

func (a *artifact) FS() fs.FS    { return os.DirFS(a.directory) }
func (a *artifact) Close() error { return os.RemoveAll(a.directory) }

type buildError struct{ cause error }

func (err buildError) Error() string { return err.cause.Error() }
func (err buildError) Unwrap() error { return err.cause }
func (err buildError) PublicErrorDetail() string {
	return "embedded image build failed; check Docker Buildx, images configuration, target platform, and layer integrity"
}

func (builder Builder) Build(ctx context.Context, request toolkitbuild.ImageBuildRequest) (_ toolkitbuild.ImageArtifact, resultErr error) {
	defer func() {
		if resultErr != nil {
			resultErr = buildError{cause: resultErr}
		}
	}()
	if ctx == nil {
		return nil, errors.New("image build context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, err := builder.Target.Normalize()
	if err != nil {
		return nil, err
	}
	configs, err := parseConfig(request.Config)
	if err != nil {
		return nil, err
	}
	references := []string{request.Manifest.Application.Image}
	for _, service := range request.Manifest.Services {
		references = append(references, service.Image)
	}
	for _, reference := range references {
		if value, ok := strings.CutPrefix(strings.TrimSpace(reference), "embed:"); ok {
			alias, digest, pinned := strings.Cut(value, "@")
			if pinned {
				if _, err := oci.ParseDigest(digest); err != nil {
					return nil, errors.New("embedded image references must use a valid sha256 digest")
				}
			}
			if _, exists := configs[alias]; !exists {
				return nil, fmt.Errorf("embedded alias %q is missing from lzc-build.yml images", alias)
			}
		}
	}
	directory, err := os.MkdirTemp("", "lazycat-image-build-*")
	if err != nil {
		return nil, errors.New("cannot create embedded image staging directory")
	}
	defer func() {
		if resultErr != nil {
			_ = os.RemoveAll(directory)
		}
	}()
	destination, err := layout.Write(filepath.Join(directory, "images"), empty.Index)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(directory, "exports"), 0o700); err != nil {
		return nil, err
	}
	lock := oci.Lock{Version: 1, Images: map[string]oci.LockImage{}}
	for _, alias := range slices.Sorted(maps.Keys(configs)) {
		cfg := configs[alias]
		dockerfile, contextDir, err := resolveBuildPaths(request.Root, cfg)
		if err != nil {
			return nil, fmt.Errorf("images.%s: %w", alias, err)
		}
		if cfg.Content != "" {
			dockerfile = filepath.Join(directory, alias+".Dockerfile")
			if err := os.WriteFile(dockerfile, []byte(cfg.Content), 0o600); err != nil {
				return nil, err
			}
		}
		source := filepath.Join(directory, "exports", alias)
		command := Command{Dir: request.Root, Env: builder.Environment, Args: []string{
			"buildx", "build", "--platform", target.Platform(), "--file", dockerfile,
			"--provenance=false", "--sbom=false",
			"--output", "type=oci,dest=" + source + ",tar=false,oci-mediatypes=true,compression=gzip,force-compression=true", contextDir,
		}}
		if builder.Logger != nil {
			builder.Logger.Info("embedded Docker image build started", "alias", alias, "target", target.Platform())
		}
		run := builder.Run
		if run == nil {
			run = builder.run
		}
		if err := run(ctx, command); err != nil {
			return nil, fmt.Errorf("images.%s: %w", alias, err)
		}
		image, err := readSingleImage(source)
		if err != nil {
			return nil, fmt.Errorf("images.%s: invalid Buildx OCI export: %w", alias, err)
		}
		if err := validateImage(ctx, image, target); err != nil {
			return nil, fmt.Errorf("images.%s: %w", alias, err)
		}
		if err := destination.AppendImage(image, layout.WithAnnotations(map[string]string{oci.AnnotationRefName: alias})); err != nil {
			return nil, err
		}
		imageManifest, err := image.Manifest()
		if err != nil {
			return nil, err
		}
		imageID, err := oci.ParseDigest(imageManifest.Config.Digest.String())
		if err != nil {
			return nil, err
		}
		entry := oci.LockImage{ImageID: imageID, Layers: make([]oci.LockLayer, 0, len(imageManifest.Layers))}
		for _, layer := range imageManifest.Layers {
			digest, err := oci.ParseDigest(layer.Digest.String())
			if err != nil {
				return nil, err
			}
			entry.Layers = append(entry.Layers, oci.LockLayer{Digest: digest, Source: oci.LayerSourceEmbed})
		}
		lock.Images[alias] = entry
		if builder.Logger != nil {
			builder.Logger.Info("embedded Docker image verified", "alias", alias, "target", target.Platform(), "layers", len(entry.Layers))
		}
	}
	file, err := os.Create(filepath.Join(directory, "images.lock"))
	if err != nil {
		return nil, err
	}
	writeErr := oci.WriteLock(ctx, file, lock)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return nil, err
	}
	// Export intermediates and inline Dockerfiles must not be copied into the LPK.
	if err := os.RemoveAll(filepath.Join(directory, "exports")); err != nil {
		return nil, err
	}
	for alias, cfg := range configs {
		if cfg.Content != "" {
			if err := os.Remove(filepath.Join(directory, alias+".Dockerfile")); err != nil {
				return nil, err
			}
		}
	}
	if _, err := oci.Validate(ctx, os.DirFS(directory)); err != nil {
		return nil, err
	}
	return &artifact{directory: directory}, nil
}

func (builder Builder) run(ctx context.Context, command Command) error {
	process := exec.CommandContext(ctx, "docker", command.Args...)
	process.Dir, process.Env = command.Dir, command.Env
	process.Stdout, process.Stderr = builder.Stdout, builder.Stderr
	if err := process.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("Docker and Docker Buildx are required for embedded image builds")
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return fmt.Errorf("Docker Buildx failed with exit code %d", exit.ExitCode())
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("could not start Docker Buildx")
	}
	return nil
}

func parseConfig(value any) (map[string]imageConfig, error) {
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, errors.New("invalid images configuration")
	}
	var nodes map[string]yaml.Node
	if err := yaml.Unmarshal(data, &nodes); err != nil || len(nodes) == 0 {
		return nil, errors.New("images must be a nonempty alias mapping")
	}
	configs := make(map[string]imageConfig, len(nodes))
	for alias, node := range nodes {
		if !aliasPattern.MatchString(alias) {
			return nil, errors.New("image alias must use letters, digits, dot, underscore, or hyphen and begin with a letter or digit")
		}
		cfg := imageConfig{}
		if node.Kind == yaml.ScalarNode && node.Tag == "!!str" {
			cfg.Dockerfile = node.Value
		} else {
			encoded, err := yaml.Marshal(&node)
			if err != nil {
				return nil, err
			}
			decoder := yaml.NewDecoder(bytes.NewReader(encoded))
			decoder.KnownFields(true)
			if err := decoder.Decode(&cfg); err != nil {
				return nil, fmt.Errorf("images.%s: invalid or unsupported image build field", alias)
			}
		}
		cfg.Dockerfile, cfg.Context = strings.TrimSpace(cfg.Dockerfile), strings.TrimSpace(cfg.Context)
		if (cfg.Dockerfile == "") == (strings.TrimSpace(cfg.Content) == "") {
			return nil, fmt.Errorf("images.%s: specify exactly one of dockerfile or dockerfile-content", alias)
		}
		if cfg.Builder != "" && strings.ToLower(strings.TrimSpace(cfg.Builder)) != "local" {
			return nil, fmt.Errorf("images.%s: only builder: local is supported", alias)
		}
		if cfg.UpstreamMatch != nil {
			return nil, fmt.Errorf("images.%s: upstream-match is unsupported; local builds embed every layer", alias)
		}
		configs[alias] = cfg
	}
	return configs, nil
}

func resolveBuildPaths(root string, cfg imageConfig) (string, string, error) {
	contextName := cfg.Context
	if contextName == "" {
		if cfg.Dockerfile != "" {
			contextName = filepath.Dir(cfg.Dockerfile)
		} else {
			contextName = "."
		}
	}
	contextDir, err := projectPath(root, contextName, true)
	if err != nil {
		return "", "", fmt.Errorf("invalid context: %w", err)
	}
	dockerfile := ""
	if cfg.Dockerfile != "" {
		dockerfile, err = projectPath(root, cfg.Dockerfile, false)
		if err != nil {
			return "", "", fmt.Errorf("invalid dockerfile: %w", err)
		}
		relative, err := filepath.Rel(contextDir, dockerfile)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", "", errors.New("Dockerfile must remain inside its build context")
		}
	}
	return dockerfile, contextDir, nil
}

func projectPath(root, name string, directory bool) (string, error) {
	if filepath.IsAbs(name) {
		return "", errors.New("path must be project-relative")
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", errors.New("project root is unavailable")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, name))
	if err != nil {
		return "", errors.New("path does not exist")
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes project root")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", errors.New("path is unavailable")
	}
	if directory && !info.IsDir() || !directory && !info.Mode().IsRegular() {
		return "", errors.New("path has the wrong file type")
	}
	return resolved, nil
}

func readSingleImage(directory string) (v1.Image, error) {
	index, err := layout.ImageIndexFromPath(directory)
	if err != nil {
		return nil, err
	}
	return singleImage(index, 0)
}
func singleImage(index v1.ImageIndex, depth int) (v1.Image, error) {
	if depth > 4 {
		return nil, errors.New("OCI index nesting is too deep")
	}
	manifest, err := index.IndexManifest()
	if err != nil {
		return nil, err
	}
	if len(manifest.Manifests) != 1 {
		return nil, errors.New("expected exactly one image in OCI export")
	}
	descriptor := manifest.Manifests[0]
	if descriptor.MediaType == types.OCIImageIndex {
		child, err := index.ImageIndex(descriptor.Digest)
		if err != nil {
			return nil, err
		}
		return singleImage(child, depth+1)
	}
	if descriptor.MediaType != types.OCIManifestSchema1 {
		return nil, errors.New("expected an OCI image manifest")
	}
	return index.Image(descriptor.Digest)
}
