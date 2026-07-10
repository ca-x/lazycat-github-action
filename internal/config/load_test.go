package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/config"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
		check   func(*testing.T, config.Config)
	}{
		{
			name: "minimal git project",
			yaml: `version: 1
project:
  output: dist/app.lpk
update:
  version_source:
    type: git
`,
			check: func(t *testing.T, got config.Config) {
				if got.Project.Root != "." || got.Project.BuildConfig != "lzc-build.yml" || got.Project.PackageFile != "package.yml" {
					t.Fatalf("defaults=%#v", got.Project)
				}
				if got.Project.Output != "dist/app.lpk" {
					t.Fatalf("output=%q", got.Project.Output)
				}
				if got.Update.Strategy != config.StrategyPull {
					t.Fatalf("strategy=%q", got.Update.Strategy)
				}
				if !got.Build.ShouldRunBuildScript() {
					t.Fatal("buildscript should default to enabled")
				}
			},
		},
		{
			name: "all future image fields are accepted",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
    channel: nightly
    sort: created
    tag_regex: '^nightly'
    exclude_regex: arm
    version_regex: '^(?P<version>.+)$'
    version_template: '{version}'
    delivery:
      mode: mirror
      image_template: mirror.example/acme/web:{tag}
      require_digest_match: true
`,
		},
		{
			name: "unknown field",
			yaml: `version: 1
project:
  target_arch: arm64
update:
  version_source:
    type: git
`,
			wantErr: "field target_arch not found",
		},
		{
			name: "image source unavailable in milestone one",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: image
    image: web
`,
			wantErr: "version source image",
		},
		{
			name: "path escapes root",
			yaml: `version: 1
project:
  output: ../app.lpk
update:
  version_source:
    type: git
`,
			wantErr: "output must remain beneath project root",
		},
		{
			name: "duplicate toolchains",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
build:
  toolchains:
    - kind: go
    - kind: GO
`,
			wantErr: "duplicate toolchain kind",
		},
		{
			name: "duplicate image IDs",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
images:
  - id: web
  - id: web
`,
			wantErr: "duplicate image id",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "lazycat.yml")
			if err := os.WriteFile(filename, []byte(test.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := config.Load(filename)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("err=%v want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.check != nil {
				test.check(t, got)
			}
		})
	}
}

func TestLoadRejectsOversizedConfiguration(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "lazycat.yml")
	data := []byte("version: 1\n#" + strings.Repeat("x", (1<<20)+1))
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(filename); err == nil {
		t.Fatal("expected oversized configuration to fail")
	}
}
