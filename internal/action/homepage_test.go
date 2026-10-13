package action_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/action"
	actionbuild "github.com/ca-x/lazycat-github-action/internal/build"
	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/platform"
	"github.com/ca-x/lazycat-github-action/internal/project"
	"github.com/ca-x/lazycat-github-action/internal/yamledit"
)

func TestRepositoryHomepageSynchronizationIsBestEffort(t *testing.T) {
	for _, test := range []struct {
		name, event                             string
		dry, disabled, failedBuild, syncFailure bool
		wantCalls                               int
	}{
		{name: "default enabled", wantCalls: 1},
		{name: "API failure does not fail build", syncFailure: true, wantCalls: 1},
		{name: "disabled", disabled: true},
		{name: "dry run", dry: true},
		{name: "failed build", failedBuild: true},
		{name: "pull request", event: "pull_request"},
		{name: "pull request target", event: "pull_request_target"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			cfg := gitConfig()
			if test.disabled {
				disabled := false
				cfg.Update.SyncHomepage = &disabled
			}
			built := false
			calls := 0
			deps := action.Dependencies{Host: platform.Host{OS: "linux", Arch: "amd64"}, ResultDir: filepath.Join(root, "results"),
				LoadConfig: func(string) (config.Config, error) { return cfg, nil },
				Inspect: func(context.Context, config.Project) (project.Info, error) {
					return project.Info{Root: root, Version: "1.0.0", PackageID: "cloud.lazycat.test", Homepage: "https://upstream.example/app"}, nil
				},
				SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
				Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
					built = true
					if test.failedBuild {
						return actionbuild.Result{}, errors.New("build failed")
					}
					return actionbuild.Result{Version: "1.0.0", PackageID: "cloud.lazycat.test", TargetPlatform: "linux/amd64"}, nil
				},
				EnsureRepoHomepage: func(_ context.Context, homepage string) (bool, error) {
					calls++
					if !built {
						t.Fatal("repository was edited before build succeeded")
					}
					if homepage != "https://upstream.example/app" {
						t.Fatalf("homepage=%s", homepage)
					}
					if test.syncFailure {
						return false, errors.New("403")
					}
					return true, nil
				},
			}
			result, err := action.Run(t.Context(), action.Input{Operation: action.OperationBuild, Version: "1.0.0", DryRun: test.dry, EventName: test.event}, deps)
			if (err != nil) != test.failedBuild || calls != test.wantCalls {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
			if err == nil && result.Changed {
				t.Fatal("GitHub metadata incorrectly marked package files changed")
			}
		})
	}
}
