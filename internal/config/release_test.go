package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/config"
)

func TestReleaseConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, release         string
		wantDisabled, wantErr bool
	}{
		{name: "default enabled"},
		{name: "custom template and repository", release: "release:\n  changelog_template: '更新至 {{.Version}}'\n  github_repository: acme/app\n"},
		{name: "explicitly disabled", release: "release:\n  use_github_release_notes: false\n", wantDisabled: true},
		{name: "GitHub URL", release: "release:\n  github_repository: https://github.com/acme/app\n"},
		{name: "invalid template", release: "release:\n  changelog_template: '{{'\n", wantErr: true},
		{name: "unknown variable", release: "release:\n  changelog_template: '{{.DoesNotExist}}'\n", wantErr: true},
		{name: "invalid repository", release: "release:\n  github_repository: https://elsewhere.example/acme/app\n", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "config.yml")
			if err := os.WriteFile(file, []byte("version: 1\nupdate:\n  version_source:\n    type: git\n"+test.release), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := config.Load(file)
			if (err != nil) != test.wantErr {
				t.Fatalf("Load()=%#v, %v", got, err)
			}
			if err == nil && got.Release.ShouldUseGitHubReleaseNotes() == test.wantDisabled {
				t.Fatalf("release=%#v", got.Release)
			}
		})
	}
}

func TestStoreChangelogFormatConfiguration(t *testing.T) {
	for _, test := range []struct {
		format, want string
		invalid      bool
	}{
		{want: "plain"}, {format: "plain", want: "plain"}, {format: "markdown", want: "markdown"}, {format: "  MARKDOWN  ", want: "markdown"}, {format: "html", invalid: true},
	} {
		t.Run(test.format, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "config.yml")
			source := "version: 1\nupdate:\n  version_source:\n    type: git\nrelease:\n  store_changelog_format: '" + test.format + "'\n"
			if err := os.WriteFile(file, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := config.Load(file)
			if test.invalid {
				if err == nil {
					t.Fatal("invalid format was accepted")
				}
				return
			}
			if err != nil || got.Release.StoreChangelogFormat != test.want {
				t.Fatalf("format=%q err=%v want=%q", got.Release.StoreChangelogFormat, err, test.want)
			}
		})
	}
}
