package imagebuild

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/platform"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	toolkitbuild "github.com/lib-x/lzc-toolkit-go/build"
	"github.com/lib-x/lzc-toolkit-go/oci"
)

func tarContent(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	archive := tar.NewWriter(&output)
	payload := bytes.Repeat([]byte("source payload\n"), 8192)
	if err := archive.WriteHeader(&tar.Header{Name: "payload.txt", Mode: 0o644, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func testImage(t *testing.T, arch string, tarData []byte) v1.Image {
	t.Helper()
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(tarData)), nil }, tarball.WithMediaType(types.OCILayer))
	if err != nil {
		t.Fatal(err)
	}
	image, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatal(err)
	}
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	config.OS, config.Architecture = "linux", arch
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		t.Fatal(err)
	}
	return mutate.ConfigMediaType(mutate.MediaType(image, types.OCIManifestSchema1), types.OCIConfigJSON)
}

func exportImage(t *testing.T, image v1.Image, command Command) error {
	t.Helper()
	for i, arg := range command.Args {
		if arg != "--output" {
			continue
		}
		dest := ""
		for field := range strings.SplitSeq(command.Args[i+1], ",") {
			if value, ok := strings.CutPrefix(field, "dest="); ok {
				dest = value
			}
		}
		if dest == "" {
			t.Fatal("OCI destination is missing")
		}
		output, err := layout.Write(dest, empty.Index)
		if err != nil {
			return err
		}
		return output.AppendImage(image)
	}
	t.Fatal("OCI exporter was not configured")
	return nil
}

func TestBuilderCreatesFullyEmbeddedArtifacts(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			data := tarContent(t)
			image := testImage(t, arch, data)
			calls := 0
			builder := Builder{Target: platform.Target{Arch: arch}, Run: func(_ context.Context, command Command) error {
				calls++
				if !strings.Contains(strings.Join(command.Args, " "), "--platform linux/"+arch) || !strings.Contains(strings.Join(command.Args, " "), "tar=false") {
					t.Fatalf("command=%v", command.Args)
				}
				if command.Args[len(command.Args)-1] != root {
					t.Fatalf("context=%v", command.Args)
				}
				return exportImage(t, image, command)
			}}
			artifact, err := builder.Build(t.Context(), toolkitbuild.ImageBuildRequest{Root: root, Config: map[string]any{"app": "Dockerfile", "worker": map[string]any{"dockerfile-content": "FROM scratch\n", "builder": "local"}}})
			if err != nil {
				t.Fatal(err)
			}
			defer artifact.Close()
			report, err := oci.Validate(t.Context(), artifact.FS())
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 || report.ImageCount != 2 || report.UpstreamLayerCount != 0 || report.EmbeddedLayerCount != 1 {
				t.Fatalf("calls=%d report=%+v", calls, report)
			}
			file, err := artifact.FS().Open("images.lock")
			if err != nil {
				t.Fatal(err)
			}
			lock, err := oci.ReadLock(t.Context(), file)
			_ = file.Close()
			if err != nil {
				t.Fatal(err)
			}
			for alias, entry := range lock.Images {
				if entry.Upstream != "" || len(entry.Layers) != 1 || entry.Layers[0].Source != oci.LayerSourceEmbed {
					t.Fatalf("alias=%s entry=%+v", alias, entry)
				}
			}
			entries, err := fs.ReadDir(artifact.FS(), ".")
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 2 || entries[0].Name() != "images" || entries[1].Name() != "images.lock" {
				t.Fatalf("intermediate files leaked into artifact: %v", entries)
			}
		})
	}
}

func TestBuilderRejectsInvalidOrTruncatedLayers(t *testing.T) {
	valid := tarContent(t)
	for _, test := range []struct {
		name string
		arch string
		data []byte
		want string
	}{
		{name: "wrong platform", arch: "arm64", data: valid, want: "does not match requested"},
		{name: "truncated file body but complete gzip", arch: "amd64", data: valid[:600], want: "incomplete or invalid"},
		{name: "invalid tar header", arch: "amd64", data: []byte(strings.Repeat("x", 1024)), want: "incomplete or invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			image := testImage(t, test.arch, test.data)
			if err := validateImage(t.Context(), image, platform.Target{OS: "linux", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	image := testImage(t, "amd64", valid)
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	config.RootFS.DiffIDs[0] = v1.Hash{Algorithm: "sha256", Hex: strings.Repeat("0", 64)}
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateImage(t.Context(), image, platform.Target{OS: "linux", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), "diff ID mismatch") {
		t.Fatalf("err=%v", err)
	}
}

func TestConfigurationAndPathValidation(t *testing.T) {
	for _, value := range []any{
		[]string{"Dockerfile"}, map[string]any{"../escape": "Dockerfile"}, map[string]any{"app": map[string]any{"dockerfile": "Dockerfile", "builder": "remote"}},
		map[string]any{"app": map[string]any{"dockerfile": "Dockerfile", "dockerfile-content": "FROM scratch"}},
		map[string]any{"app": map[string]any{"dockerfile": "Dockerfile", "upstream-match": "registry.lazycat.cloud"}},
		map[string]any{"app": map[string]any{"dockerfile": "Dockerfile", "invalid-key": "value"}},
	} {
		if _, err := parseConfig(value); err == nil {
			t.Fatalf("accepted %v", value)
		}
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "Dockerfile"), []byte("FROM scratch"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{outside, "../", "escape"} {
		if _, err := projectPath(root, name, true); err == nil {
			t.Fatalf("accepted path %s", name)
		}
	}
}

func TestBuilderFailsWithoutArtifactAndCleansStaging(t *testing.T) {
	root := t.TempDir()
	source := ""
	builder := Builder{Run: func(_ context.Context, command Command) error {
		for _, arg := range command.Args {
			for field := range strings.SplitSeq(arg, ",") {
				if dest, ok := strings.CutPrefix(field, "dest="); ok {
					source = dest
				}
			}
		}
		return errors.New("build failed")
	}}
	result, err := builder.Build(t.Context(), toolkitbuild.ImageBuildRequest{Root: root, Config: map[string]any{"app": map[string]any{"dockerfile-content": "FROM scratch"}}})
	if err == nil || result != nil || source == "" {
		t.Fatalf("result=%v err=%v source=%s", result, err, source)
	}
	if _, err := os.Stat(filepath.Dir(filepath.Dir(source))); !os.IsNotExist(err) {
		t.Fatalf("staging was not removed: %v", err)
	}
}

func TestMissingManifestAliasFailsBeforeDocker(t *testing.T) {
	builder := Builder{Run: func(context.Context, Command) error { t.Fatal("Docker was called for a missing alias"); return nil }}
	request := toolkitbuild.ImageBuildRequest{Root: t.TempDir(), Config: map[string]any{"app": map[string]any{"dockerfile-content": "FROM scratch"}}}
	request.Manifest.Application.Image = "embed:missing"
	if _, err := builder.Build(t.Context(), request); err == nil || !strings.Contains(err.Error(), "missing from lzc-build.yml") {
		t.Fatalf("err=%v", err)
	}
}
