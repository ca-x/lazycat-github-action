package build_test

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	actionbuild "github.com/ca-x/lazycat-github-action/internal/build"
	"github.com/ca-x/lazycat-github-action/internal/imagebuild"
	"github.com/ca-x/lazycat-github-action/internal/platform"
	"github.com/ca-x/lazycat-github-action/internal/project"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/lib-x/lzc-toolkit-go/lpk"
	"github.com/lib-x/lzc-toolkit-go/oci"
)

func embeddedFixture(t *testing.T) (project.Info, []byte) {
	t.Helper()
	root := t.TempDir()
	payload := bytes.Repeat([]byte("complete layer payload\n"), 8192)
	files := map[string][]byte{
		"package.yml":      []byte("package: cloud.lazycat.action.embedded\nversion: 1.0.0\nname: Embedded Fixture\ndescription: Docker source fixture\nmin_os_version: 1.5.0\n"),
		"lzc-manifest.yml": []byte("application:\n  subdomain: embedded\n  upstreams:\n    - location: /\n      backend: http://app:8080/\nservices:\n  app:\n    image: embed:runtime\n"),
		"lzc-build.yml":    []byte("manifest: lzc-manifest.yml\nimages:\n  runtime:\n    dockerfile: Dockerfile\n    context: .\n    builder: local\n"),
		"Dockerfile":       []byte("FROM scratch\nCOPY payload.txt /payload.txt\n"),
		"payload.txt":      payload,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return project.Info{Root: root, PackageFile: filepath.Join(root, "package.yml"), ManifestFile: filepath.Join(root, "lzc-manifest.yml"), BuildConfig: filepath.Join(root, "lzc-build.yml"), Output: filepath.Join(root, "dist", "app.lpk"), PackageID: "cloud.lazycat.action.embedded", Version: "1.0.0"}, payload
}

func fixtureImage(t *testing.T, payload []byte, arch string) v1.Image {
	t.Helper()
	var data bytes.Buffer
	archive := tar.NewWriter(&data)
	if err := archive.WriteHeader(&tar.Header{Name: "payload.txt", Mode: 0o644, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data.Bytes())), nil }, tarball.WithMediaType(types.OCILayer))
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

func inspectEmbeddedLPK(t *testing.T, result actionbuild.Result, payload []byte, arch string) {
	t.Helper()
	reader, err := lpk.OpenFile(t.Context(), result.Path)
	if err != nil {
		t.Fatal(err)
	}
	extraction := filepath.Join(t.TempDir(), "package")
	if err := reader.Extract(t.Context(), extraction); err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	report, err := oci.Validate(t.Context(), os.DirFS(extraction))
	if err != nil {
		t.Fatal(err)
	}
	if report.ImageCount != 1 || report.EmbeddedLayerCount == 0 || report.UpstreamLayerCount != 0 || report.EmbeddedBytes == 0 {
		t.Fatalf("report=%+v", report)
	}
	data, err := os.ReadFile(filepath.Join(extraction, "manifest.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "embed:runtime@"+report.ResolvedByAlias["runtime"]) {
		t.Fatalf("manifest=%s", data)
	}
	source, err := layout.FromPath(filepath.Join(extraction, "images"))
	if err != nil {
		t.Fatal(err)
	}
	index, err := source.ImageIndex()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := index.IndexManifest()
	if err != nil {
		t.Fatal(err)
	}
	image, err := index.Image(manifest.Manifests[0].Digest)
	if err != nil {
		t.Fatal(err)
	}
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	if config.OS != "linux" || config.Architecture != arch {
		t.Fatalf("platform=%s/%s", config.OS, config.Architecture)
	}
	layers, err := image.Layers()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, layer := range layers {
		file, err := layer.Uncompressed()
		if err != nil {
			t.Fatal(err)
		}
		tarReader := tar.NewReader(file)
		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimPrefix(header.Name, "/") == "payload.txt" {
				content, err := io.ReadAll(tarReader)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(content, payload) {
					t.Fatalf("layer payload was truncated: %d bytes, want %d", len(content), len(payload))
				}
				found = true
			}
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if !found {
		t.Fatal("built image did not contain payload.txt")
	}
}

func TestBuilderEmbedsOCIArtifactWithoutRegistry(t *testing.T) {
	info, payload := embeddedFixture(t)
	image := fixtureImage(t, payload, "amd64")
	adapter := imagebuild.Builder{Run: func(_ context.Context, command imagebuild.Command) error {
		destination := ""
		for _, arg := range command.Args {
			for field := range strings.SplitSeq(arg, ",") {
				if value, ok := strings.CutPrefix(field, "dest="); ok {
					destination = value
				}
			}
		}
		if destination == "" {
			t.Fatal("Buildx was not invoked with an OCI destination")
		}
		output, err := layout.Write(destination, empty.Index)
		if err != nil {
			return err
		}
		return output.AppendImage(image)
	}}
	result, err := (actionbuild.Builder{}).Build(t.Context(), actionbuild.Request{Project: info, Version: "1.0.0", ImageBuilder: adapter})
	if err != nil {
		t.Fatal(err)
	}
	inspectEmbeddedLPK(t, result, payload, "amd64")
	data, err := os.ReadFile(info.ManifestFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "@sha256:") {
		t.Fatal("source manifest was changed by image embedding")
	}
}

// This test is mandatory in the Docker-enabled CI job; ordinary unit runs need no daemon.
func TestDockerEmbeddedLPK(t *testing.T) {
	if os.Getenv("LAZYCAT_TEST_DOCKER") != "1" {
		t.Skip("enable LAZYCAT_TEST_DOCKER=1 on a Docker/Buildx runner")
	}
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			info, payload := embeddedFixture(t)
			result, err := (actionbuild.Builder{}).Build(t.Context(), actionbuild.Request{Project: info, Version: "1.0.0", Target: platform.Target{Arch: arch}})
			if err != nil {
				t.Fatal(err)
			}
			inspectEmbeddedLPK(t, result, payload, arch)
		})
	}
}

func TestDefaultImageBuilderFiltersProtectedEnvironment(t *testing.T) {
	info, _ := embeddedFixture(t)
	binaries := t.TempDir()
	script := `#!/bin/sh
printf 'store=%s github=%s version=%s platform=%s\n' "${APPSTORE_TOKEN-unset}" "${GITHUB_TOKEN-unset}" "${LAZYCAT_VERSION-unset}" "${LAZYCAT_TARGET_PLATFORM-unset}"
exit 7
`
	if err := os.WriteFile(filepath.Join(binaries, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binaries+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("APPSTORE_TOKEN", "private-store-secret")
	t.Setenv("GITHUB_TOKEN", "github-secret")
	var output bytes.Buffer
	_, err := (actionbuild.Builder{Stdout: &output}).Build(t.Context(), actionbuild.Request{Project: info, Version: "1.0.0"})
	if err == nil {
		t.Fatal("failed Docker build unexpectedly succeeded")
	}
	if !strings.Contains(output.String(), "store=unset github=unset version=1.0.0 platform=linux/amd64") {
		t.Fatalf("environment was not protected: %s", output.String())
	}
	if strings.Contains(err.Error(), "private-store-secret") || strings.Contains(err.Error(), "github-secret") {
		t.Fatalf("error exposed credentials: %v", err)
	}
	if !strings.Contains(err.Error(), "check Docker Buildx") {
		t.Fatalf("missing actionable image-build diagnostic: %v", err)
	}
	if _, err := os.Stat(info.Output); !os.IsNotExist(err) {
		t.Fatal("failed build published an LPK")
	}
}
