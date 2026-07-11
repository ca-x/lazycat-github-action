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
				if got.Update.AllowDowngrade {
					t.Fatal("allow_downgrade should default to false")
				}
				if !got.Build.ShouldRunBuildScript() {
					t.Fatal("buildscript should default to enabled")
				}
				if strings.Join(got.Stores.Official.Locales, ",") != "zh,en" {
					t.Fatalf("official locales=%v", got.Stores.Official.Locales)
				}
			},
		},
		{
			name: "store metadata is normalized",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    enabled: true
    skip_if_version_exists: true
    create_if_missing: true
    changelog_locales: [ZH, en, zh]
    application:
      language: ZH
      name: " Example "
      source: " https://github.com/acme/example "
      source_author: " Acme "
  private:
    enabled: true
    skip_if_version_exists: true
    name: " Private Example "
    summary: " Private summary "
`,
			check: func(t *testing.T, got config.Config) {
				if !got.Stores.Official.SkipIfVersionExists || !got.Stores.Private.SkipIfVersionExists {
					t.Fatalf("store deduplication=%#v", got.Stores)
				}
				if strings.Join(got.Stores.Official.Locales, ",") != "zh,en" {
					t.Fatalf("official locales=%v", got.Stores.Official.Locales)
				}
				if got.Stores.Official.Application.Language != "zh" || got.Stores.Official.Application.Name != "Example" {
					t.Fatalf("official application=%#v", got.Stores.Official.Application)
				}
				if got.Stores.Private.Name != "Private Example" || got.Stores.Private.Summary != "Private summary" {
					t.Fatalf("private store=%#v", got.Stores.Private)
				}
			},
		},
		{
			name: "store deduplication defaults off",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    enabled: true
  private:
    enabled: true
`,
			check: func(t *testing.T, got config.Config) {
				if got.Stores.Official.SkipIfVersionExists || got.Stores.Private.SkipIfVersionExists {
					t.Fatalf("store deduplication should default off: %#v", got.Stores)
				}
			},
		},
		{
			name: "unknown store deduplication field",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    skip_when_version_exists: true
`,
			wantErr: "field skip_when_version_exists not found",
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
			name: "image version source",
			yaml: `version: 1
project: {}
update:
  allow_downgrade: true
  version_source:
    type: image
    image: web
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
`,
			check: func(t *testing.T, got config.Config) {
				if !got.Update.AllowDowngrade {
					t.Fatal("allow_downgrade should preserve explicit true")
				}
				image := got.Images[0]
				if image.Channel != "stable" || image.Sort != "semver" || image.VersionTemplate != "{version}" || image.Delivery.Mode != "lazycat" {
					t.Fatalf("image defaults=%#v", image)
				}
			},
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
			name: "unknown toolchain",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
build:
  toolchains:
    - kind: python
`,
			wantErr: "unsupported toolchain kind",
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
    target: service
    service: web
    source: ghcr.io/acme/web
  - id: web
    target: application
    source: ghcr.io/acme/runtime
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

func TestLoadRejectsInvalidImageConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		image   string
		stores  string
		wantErr string
	}{
		{name: "missing source", image: "id: web\ntarget: service\nservice: web", wantErr: "source is required"},
		{name: "unsafe id", image: "id: web/../../main\ntarget: service\nservice: web\nsource: ghcr.io/acme/web", wantErr: "must use letters"},
		{name: "service missing name", image: "id: web\ntarget: service\nsource: ghcr.io/acme/web", wantErr: "service is required"},
		{name: "application has service", image: "id: web\ntarget: application\nservice: web\nsource: ghcr.io/acme/web", wantErr: "must be empty"},
		{name: "nightly missing regex", image: "id: web\ntarget: service\nservice: web\nsource: ghcr.io/acme/web\nchannel: nightly", wantErr: "tag_regex is required"},
		{name: "custom missing sort", image: "id: web\ntarget: service\nservice: web\nsource: ghcr.io/acme/web\nchannel: custom\ntag_regex: edge", wantErr: "sort is required"},
		{name: "bad regex", image: "id: web\ntarget: service\nservice: web\nsource: ghcr.io/acme/web\ntag_regex: '['", wantErr: "invalid tag_regex"},
		{name: "mirror missing template", image: "id: web\ntarget: service\nservice: web\nsource: ghcr.io/acme/web\ndelivery:\n  mode: mirror", wantErr: "image_template is required"},
		{name: "official direct", image: "id: web\ntarget: service\nservice: web\nsource: ghcr.io/acme/web\ndelivery:\n  mode: direct", stores: "official:\n    enabled: true", wantErr: "official store requires lazycat"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			yaml := "version: 1\nproject: {}\nupdate:\n  version_source:\n    type: git\nimages:\n  - " + strings.ReplaceAll(test.image, "\n", "\n    ") + "\n"
			if test.stores != "" {
				yaml += "stores:\n" + indent(test.stores, 2)
			}
			filename := filepath.Join(t.TempDir(), "lazycat.yml")
			if err := os.WriteFile(filename, []byte(yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := config.Load(filename); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("err=%v yaml=\n%s", err, yaml)
			}
		})
	}
}

func TestLoadRejectsUnusedOfficialApplicationMetadata(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "lazycat.yml")
	data := []byte(`version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    enabled: true
    application:
      name: Example
`)
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(filename); err == nil || !strings.Contains(err.Error(), "create_if_missing") {
		t.Fatalf("err=%v", err)
	}
}

func indent(value string, spaces int) string {
	prefix := strings.Repeat(" ", spaces)
	lines := strings.Split(value, "\n")
	for index := range lines {
		lines[index] = prefix + lines[index]
	}
	return strings.Join(lines, "\n") + "\n"
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
