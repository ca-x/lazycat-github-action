package action_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/action"
	actionbuild "github.com/ca-x/lazycat-github-action/internal/build"
	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/imageflow"
	"github.com/ca-x/lazycat-github-action/internal/lpkcheck"
	"github.com/ca-x/lazycat-github-action/internal/platform"
	"github.com/ca-x/lazycat-github-action/internal/project"
	"github.com/ca-x/lazycat-github-action/internal/publishflow"
	"github.com/ca-x/lazycat-github-action/internal/releasenotes"
	"github.com/ca-x/lazycat-github-action/internal/yamledit"
)

func TestChangelogPrecedenceAndFallback(t *testing.T) {
	for _, test := range []struct {
		name, explicit, template, inputTemplate, repository, homepage, event, eventBody, body, want, wantRepo string
		disabled, failLookup                                                                                  bool
		wantCalls                                                                                             int
	}{
		{name: "explicit text stays literal", explicit: "{{.Unknown}}\nmanual notes", want: "{{.Unknown}}\nmanual notes"},
		{name: "default uses upstream", body: "## Fixed\n\n- Startup", want: "## Fixed\n\n- Startup", wantCalls: 1, wantRepo: "upstream/app"},
		{name: "default fallback", want: "Release 1.2.3", wantCalls: 1, wantRepo: "upstream/app"},
		{name: "template wraps upstream", template: "{{.Name}} {{.Tag}}\n\n{{.ReleaseNotes}}", body: "Fixed", want: "Example v1.2.3\n\nFixed", wantCalls: 1, wantRepo: "upstream/app"},
		{name: "input overrides config template", template: "unused {{.Version}}", inputTemplate: "更新至 {{.Version}}", want: "更新至 1.2.3", wantCalls: 1, wantRepo: "upstream/app"},
		{name: "explicit repository overrides homepage", repository: "other/project", body: "Other", want: "Other", wantCalls: 1, wantRepo: "other/project"},
		{name: "current repository fallback", homepage: "https://example.com/app", body: "Current", want: "Current", wantCalls: 1, wantRepo: "current/packaging"},
		{name: "API error keeps custom fallback", failLookup: true, template: "更新至 {{.Version}}", want: "更新至 1.2.3", wantCalls: 1, wantRepo: "upstream/app"},
		{name: "lookup disabled", disabled: true, body: "Ignored", want: "Release 1.2.3"},
		{name: "disabled ignores event body", disabled: true, event: "release", eventBody: "Ignored", want: "Release 1.2.3"},
		{name: "release event body", event: "release", eventBody: "Event notes", want: "Event notes"},
		{name: "empty event body still looks up", event: "release", eventBody: " \n", body: "Upstream", want: "Upstream", wantCalls: 1, wantRepo: "upstream/app"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := gitConfig()
			cfg.Release = config.Release{ChangelogTemplate: test.template, GitHubRepository: test.repository}
			if test.disabled {
				disabled := false
				cfg.Release.UseGitHubReleaseNotes = &disabled
			}
			deps := changelogDependencies(t, cfg, test.homepage)
			calls := 0
			deps.LookupReleaseNotes = func(_ context.Context, repo string, tags []string) (releasenotes.Release, error) {
				calls++
				if repo != test.wantRepo || !reflect.DeepEqual(tags, []string{"v1.2.3", "v1.2.3", "1.2.3"}) {
					t.Fatalf("lookup repo=%q tags=%v", repo, tags)
				}
				if test.failLookup {
					return releasenotes.Release{}, errors.New("unavailable")
				}
				return releasenotes.Release{Body: test.body}, nil
			}
			result, err := action.Run(t.Context(), action.Input{Operation: action.OperationBuild, Version: "1.2.3", Changelog: test.explicit, ChangelogTemplate: test.inputTemplate, EventName: test.event, ReleaseNotes: test.eventBody}, deps)
			if err != nil || result.Changelog != test.want || calls != test.wantCalls {
				t.Fatalf("changelog=%q err=%v calls=%d; want %q calls=%d", result.Changelog, err, calls, test.want, test.wantCalls)
			}
			data, err := os.ReadFile(result.ResultFile)
			if err != nil {
				t.Fatal(err)
			}
			var persisted action.Result
			if err := json.Unmarshal(data, &persisted); err != nil || persisted.Changelog != test.want {
				t.Fatalf("persisted=%#v err=%v", persisted, err)
			}
		})
	}
}

func TestCurrentRepositoryReleaseNotesAreNotWrappedAgain(t *testing.T) {
	cfg := gitConfig()
	cfg.Release.ChangelogTemplate = "{{.Name}} {{.Tag}}\n{{.ReleaseNotes}}"
	deps := changelogDependencies(t, cfg, "https://example.com/app")
	const published = "Example v1.2.3\n## Fixed\n- Startup"
	deps.LookupReleaseNotes = func(context.Context, string, []string) (releasenotes.Release, error) {
		return releasenotes.Release{Tag: "v1.2.3", Body: published, AssetNames: []string{"cloud.example.app-v1.2.3.lpk"}}, nil
	}
	result, err := action.Run(t.Context(), action.Input{Operation: action.OperationBuild, Version: "1.2.3"}, deps)
	if err != nil || result.Changelog != published {
		t.Fatalf("changelog=%q err=%v", result.Changelog, err)
	}
}

func TestImageCheckChangelogUsesSelectedVersion(t *testing.T) {
	cfg := gitConfig()
	cfg.Update.VersionSource = config.VersionSource{Type: config.VersionSourceImage, Image: "web"}
	deps := changelogDependencies(t, cfg, "")
	deps.CheckImages = func(_ context.Context, request imageflow.Request) (imageflow.Result, error) {
		selected := imageflow.Result{Version: "2.0.0", Changed: true}
		if request.BeforeApply == nil {
			t.Fatal("changelog preparation must run before image writes")
		}
		if err := request.BeforeApply(selected); err != nil {
			return imageflow.Result{}, err
		}
		return selected, nil
	}
	deps.LookupReleaseNotes = func(_ context.Context, _ string, tags []string) (releasenotes.Release, error) {
		if !reflect.DeepEqual(tags, []string{"v2.0.0", "v2.0.0", "2.0.0"}) {
			t.Fatalf("tags=%v", tags)
		}
		return releasenotes.Release{Body: "Version two"}, nil
	}
	result, err := action.Run(t.Context(), action.Input{Operation: action.OperationCheck}, deps)
	if err != nil || result.Version != "2.0.0" || result.Changelog != "Version two" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestPublishReceivesResolvedChangelog(t *testing.T) {
	for _, operation := range []action.Operation{action.OperationPublishOfficial, action.OperationPublishPrivate} {
		t.Run(string(operation), func(t *testing.T) {
			deps := changelogDependencies(t, gitConfig(), "")
			deps.LookupReleaseNotes = func(_ context.Context, _ string, tags []string) (releasenotes.Release, error) {
				if tags[0] != "component-v1.2.3" {
					t.Fatalf("lost component tag: %v", tags)
				}
				return releasenotes.Release{Body: "## Fixed\n\n- Issue"}, nil
			}
			called := false
			deps.Publish = func(_ context.Context, request publishflow.Request) (publishflow.Result, error) {
				called = true
				if request.Changelog != "## Fixed\n\n- Issue" {
					t.Fatalf("changelog=%q", request.Changelog)
				}
				return publishflow.Result{Artifact: lpkcheck.Result{Version: "1.2.3"}}, nil
			}
			result, err := action.Run(t.Context(), action.Input{Operation: operation, Version: "1.2.3", Tag: "component-v1.2.3"}, deps)
			if err != nil || !called || result.Changelog != "## Fixed\n\n- Issue" {
				t.Fatalf("result=%#v called=%v err=%v", result, called, err)
			}
		})
	}
}

func TestInvalidChangelogTemplateFailsBeforeProjectChanges(t *testing.T) {
	deps := changelogDependencies(t, gitConfig(), "")
	deps.Inspect = func(context.Context, config.Project) (project.Info, error) {
		t.Fatal("inspected project before rejecting template")
		return project.Info{}, nil
	}
	_, err := action.Run(t.Context(), action.Input{ChangelogTemplate: "{{.Unknown}}"}, deps)
	var actionErr *action.Error
	if !errors.As(err, &actionErr) || actionErr.Code != action.CodeConfigInvalid {
		t.Fatalf("err=%v", err)
	}
}

func changelogDependencies(t *testing.T, cfg config.Config, homepage string) action.Dependencies {
	t.Helper()
	root := t.TempDir()
	if homepage == "" {
		homepage = "https://github.com/upstream/app"
	}
	info := project.Info{Root: root, PackageID: "cloud.example.app", Version: "1.0.0", Name: "Example", Homepage: homepage, PackageFile: filepath.Join(root, "package.yml"), Output: filepath.Join(root, "app.lpk")}
	return action.Dependencies{
		Host: platform.Host{OS: "linux", Arch: "amd64"}, GitHubRepository: "current/packaging", ResultDir: filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect:    func(context.Context, config.Project) (project.Info, error) { return info, nil },
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(_ context.Context, request actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{PackageID: info.PackageID, Version: request.Version, SHA256: strings.Repeat("a", 64), TargetPlatform: "linux/amd64"}, nil
		},
	}
}

func TestOversizedExplicitChangelogFailsBeforeProjectChanges(t *testing.T) {
	deps := changelogDependencies(t, gitConfig(), "")
	deps.Inspect = func(context.Context, config.Project) (project.Info, error) {
		t.Fatal("inspected before rejecting oversized changelog")
		return project.Info{}, nil
	}
	_, err := action.Run(t.Context(), action.Input{Changelog: strings.Repeat("中", 50000)}, deps)
	var actionErr *action.Error
	if !errors.As(err, &actionErr) || actionErr.Code != action.CodeConfigInvalid {
		t.Fatalf("err=%v", err)
	}
}

func TestCurrentRepositoryNotesDistinguishRawBodiesAndFinalOverrides(t *testing.T) {
	for _, test := range []struct {
		name, inputTemplate, event string
		final                      bool
		want                       string
	}{
		{name: "manual release without LPK stays raw", want: "CONFIG 1.2.3\nFixed"},
		{name: "explicit template overrides final API body", inputTemplate: "INPUT {{.Version}}", final: true, want: "INPUT 1.2.3"},
		{name: "final release event is not wrapped", event: "release", final: true, want: "Fixed"},
		{name: "explicit template overrides final event body", event: "release", inputTemplate: "INPUT {{.Version}}", final: true, want: "INPUT 1.2.3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := gitConfig()
			cfg.Release.ChangelogTemplate = "CONFIG {{.Version}}\n{{.ReleaseNotes}}"
			deps := changelogDependencies(t, cfg, "https://example.com/app")
			var assets []string
			if test.final {
				assets = []string{"cloud.example.app-v1.2.3.lpk"}
			}
			deps.LookupReleaseNotes = func(context.Context, string, []string) (releasenotes.Release, error) {
				return releasenotes.Release{Tag: "v1.2.3", Body: "Fixed", AssetNames: assets}, nil
			}
			input := action.Input{Operation: action.OperationBuild, Version: "1.2.3", ChangelogTemplate: test.inputTemplate, EventName: test.event}
			if test.event == "release" {
				input.ReleaseNotes, input.ReleaseAssets = "Fixed", assets
			}
			result, err := action.Run(t.Context(), input, deps)
			if err != nil || result.Changelog != test.want {
				t.Fatalf("changelog=%q err=%v; want %q", result.Changelog, err, test.want)
			}
		})
	}
}

func TestSameVersionAliasNotesRemainRawForNewComponentTag(t *testing.T) {
	cfg := gitConfig()
	cfg.Release.ChangelogTemplate = "{{.Tag}}\n{{.ReleaseNotes}}"
	deps := changelogDependencies(t, cfg, "https://example.com/app")
	deps.LookupReleaseNotes = func(context.Context, string, []string) (releasenotes.Release, error) {
		return releasenotes.Release{Tag: "v1.2.3", Body: "Fixed", AssetNames: []string{"cloud.example.app-v1.2.3.lpk"}}, nil
	}
	result, err := action.Run(t.Context(), action.Input{Operation: action.OperationBuild, Version: "1.2.3", Tag: "component-v1.2.3"}, deps)
	if err != nil || result.Changelog != "component-v1.2.3\nFixed" {
		t.Fatalf("changelog=%q err=%v", result.Changelog, err)
	}
}

func TestSeparatePublicationRepositoryChangelog(t *testing.T) {
	for _, test := range []struct {
		name, tag, body, explicit, inputTemplate, event, want string
		assets                                                []string
		disabled, lookupError                                 bool
		wantPublishedCalls, wantUpstreamCalls                 int
	}{
		{name: "reuse final publication notes", tag: "v1.2.3", body: "FINAL\nFixed", assets: []string{"cloud.example.app-v1.2.3.lpk"}, want: "FINAL\nFixed", wantPublishedCalls: 1},
		{name: "reuse legacy output", tag: "v1.2.3", body: "FINAL", assets: []string{"app.lpk"}, want: "FINAL", wantPublishedCalls: 1},
		{name: "missing publication falls back to source", want: "CONFIG\nUpstream", wantPublishedCalls: 1, wantUpstreamCalls: 1},
		{name: "unrelated asset cannot mark final notes", tag: "v1.2.3", body: "Other", assets: []string{"other-v1.2.3.lpk"}, want: "CONFIG\nUpstream", wantPublishedCalls: 1, wantUpstreamCalls: 1},
		{name: "wrong tag cannot mark final notes", tag: "1.2.3", body: "Other", assets: []string{"cloud.example.app-v1.2.3.lpk"}, want: "CONFIG\nUpstream", wantPublishedCalls: 1, wantUpstreamCalls: 1},
		{name: "lookup failure falls back", lookupError: true, want: "CONFIG\nUpstream", wantPublishedCalls: 1, wantUpstreamCalls: 1},
		{name: "explicit text bypasses both clients", explicit: "Literal {{.Version}}", want: "Literal {{.Version}}"},
		{name: "disabled bypasses both clients", disabled: true, want: "CONFIG\n"},
		{name: "per run template overrides final reuse", inputTemplate: "INPUT {{.ReleaseNotes}}", want: "INPUT Upstream", wantUpstreamCalls: 1},
		{name: "source release event remains template data", event: "release", want: "CONFIG\nEvent notes", wantPublishedCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := gitConfig()
			cfg.Release.ChangelogTemplate = "CONFIG\n{{.ReleaseNotes}}"
			if test.disabled {
				enabled := false
				cfg.Release.UseGitHubReleaseNotes = &enabled
			}
			deps := changelogDependencies(t, cfg, "https://github.com/private/source")
			publishedCalls, upstreamCalls := 0, 0
			deps.LookupPublishedReleaseNotes = func(_ context.Context, repository string, tags []string) (releasenotes.Release, error) {
				publishedCalls++
				if repository != "distribution/app-releases" || !reflect.DeepEqual(tags, []string{"v1.2.3"}) {
					t.Fatalf("publication lookup=%s %v", repository, tags)
				}
				if test.lookupError {
					return releasenotes.Release{}, errors.New("unavailable")
				}
				return releasenotes.Release{Tag: test.tag, Body: test.body, AssetNames: test.assets}, nil
			}
			deps.LookupReleaseNotes = func(_ context.Context, repository string, _ []string) (releasenotes.Release, error) {
				upstreamCalls++
				if repository != "private/source" {
					t.Fatalf("upstream redirected to %s", repository)
				}
				return releasenotes.Release{Body: "Upstream"}, nil
			}
			result, err := action.Run(t.Context(), action.Input{
				Operation: action.OperationBuild, Version: "1.2.3", ReleaseRepository: "distribution/app-releases",
				Changelog: test.explicit, ChangelogTemplate: test.inputTemplate, EventName: test.event,
				ReleaseNotes: "Event notes", ReleaseAssets: []string{"cloud.example.app-v1.2.3.lpk"},
			}, deps)
			if err != nil || result.Changelog != test.want || publishedCalls != test.wantPublishedCalls || upstreamCalls != test.wantUpstreamCalls {
				t.Fatalf("changelog=%q published=%d upstream=%d err=%v", result.Changelog, publishedCalls, upstreamCalls, err)
			}
		})
	}
}

func TestInvalidPublicationRepositoryFailsBeforeProjectInspection(t *testing.T) {
	for _, repository := range []string{"https://github.com/owner/repo", "../repo", "owner/repo/extra", "owner/repo?token=secret"} {
		t.Run(repository, func(t *testing.T) {
			deps := changelogDependencies(t, gitConfig(), "")
			deps.Inspect = func(context.Context, config.Project) (project.Info, error) {
				t.Fatal("invalid publication repository reached project inspection")
				return project.Info{}, nil
			}
			_, err := action.Run(t.Context(), action.Input{ReleaseRepository: repository}, deps)
			if err == nil || !strings.Contains(err.Error(), "release-repository must be owner/repo") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
