package manifestedit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/manifestedit"
)

func TestReadAndApplyUpdatesOnlyExplicitService(t *testing.T) {
	filename := writeManifest(t, `application:
  subdomain: app
services:
  db:
    image: postgres:17
  web:
    # retain this comment
    # upstream: ghcr.io/acme/web:v1.0.0
    image: registry.lazycat.cloud/acme/web:v1.0.0 # runtime
`)
	target := manifestedit.Target{ID: "web", Kind: manifestedit.TargetService, Service: "web"}
	current, err := manifestedit.Read(filename, []manifestedit.Target{target})
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 1 || current[0].RuntimeRef != "registry.lazycat.cloud/acme/web:v1.0.0" || current[0].UpstreamRef != "ghcr.io/acme/web:v1.0.0" {
		t.Fatalf("current=%#v", current)
	}

	changes, err := manifestedit.Apply(filename, []manifestedit.Update{{
		Target: target, SourceRef: "ghcr.io/acme/web:v1.2.3", RuntimeRef: "registry.lazycat.cloud/acme/web:v1.2.3",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !changes[0].Changed || changes[0].OldUpstreamRef != "ghcr.io/acme/web:v1.0.0" {
		t.Fatalf("changes=%#v", changes)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, expected := range []string{
		"image: postgres:17",
		"# retain this comment",
		"# upstream: ghcr.io/acme/web:v1.2.3",
		"image: registry.lazycat.cloud/acme/web:v1.2.3 # runtime",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("output missing %q:\n%s", expected, got)
		}
	}
}

func TestApplyApplicationImageAndInsertMissingServiceImage(t *testing.T) {
	filename := writeManifest(t, `application:
  subdomain: app
services:
  worker:
    command: run
`)
	changes, err := manifestedit.Apply(filename, []manifestedit.Update{
		{Target: manifestedit.Target{ID: "runtime", Kind: manifestedit.TargetApplication}, SourceRef: "ghcr.io/acme/runtime:v1", RuntimeRef: "mirror/acme/runtime:v1"},
		{Target: manifestedit.Target{ID: "worker", Kind: manifestedit.TargetService, Service: "worker"}, SourceRef: "ghcr.io/acme/worker:v1", RuntimeRef: "ghcr.io/acme/worker:v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || !changes[0].Changed || !changes[1].Changed {
		t.Fatalf("changes=%#v", changes)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, expected := range []string{"upstream: ghcr.io/acme/runtime:v1", "image: mirror/acme/runtime:v1", "upstream: ghcr.io/acme/worker:v1", "image: ghcr.io/acme/worker:v1"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("output missing %q:\n%s", expected, got)
		}
	}
}

func TestApplyValidatesAllTargetsBeforeWriting(t *testing.T) {
	original := `application:
  subdomain: app
services:
  web:
    image: old
`
	filename := writeManifest(t, original)
	_, err := manifestedit.Apply(filename, []manifestedit.Update{
		{Target: manifestedit.Target{ID: "web", Kind: manifestedit.TargetService, Service: "web"}, SourceRef: "source:new", RuntimeRef: "runtime:new"},
		{Target: manifestedit.Target{ID: "missing", Kind: manifestedit.TargetService, Service: "missing"}, SourceRef: "source:new", RuntimeRef: "runtime:new"},
	})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err=%v", err)
	}
	data, readErr := os.ReadFile(filename)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != original {
		t.Fatalf("manifest changed after validation failure:\n%s", data)
	}
}

func TestReadRejectsDuplicateTargetsAndSymlink(t *testing.T) {
	filename := writeManifest(t, "application:\n  subdomain: app\n")
	target := manifestedit.Target{ID: "runtime", Kind: manifestedit.TargetApplication}
	if _, err := manifestedit.Read(filename, []manifestedit.Target{target, target}); err == nil {
		t.Fatal("expected duplicate target to fail")
	}
	symlink := filepath.Join(t.TempDir(), "manifest.yml")
	if err := os.Symlink(filename, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := manifestedit.Read(symlink, []manifestedit.Target{target}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("err=%v", err)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	filename := writeManifest(t, `application:
  # upstream: ghcr.io/acme/runtime:v1
  image: mirror/acme/runtime:v1
  subdomain: app
`)
	changes, err := manifestedit.Apply(filename, []manifestedit.Update{{
		Target:    manifestedit.Target{ID: "runtime", Kind: manifestedit.TargetApplication},
		SourceRef: "ghcr.io/acme/runtime:v1", RuntimeRef: "mirror/acme/runtime:v1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Changed {
		t.Fatalf("changes=%#v", changes)
	}
}

func writeManifest(t *testing.T, contents string) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "lzc-manifest.yml")
	if err := os.WriteFile(filename, []byte(contents), 0o640); err != nil {
		t.Fatal(err)
	}
	return filename
}
