package action_test

import (
	"context"
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
	"github.com/ca-x/lazycat-github-action/internal/store/official"
	"github.com/ca-x/lazycat-github-action/internal/yamledit"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

func TestRunBuildCallsDependenciesInOrderAndReturnsStableResult(t *testing.T) {
	var calls []string
	root := t.TempDir()
	packageFile := filepath.Join(root, "package.yml")
	manifestFile := filepath.Join(root, "lzc-manifest.yml")
	deps := action.Dependencies{
		Host:      platform.Host{OS: "linux", Arch: "arm64"},
		ResultDir: filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) {
			calls = append(calls, "load")
			return gitConfig(), nil
		},
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			calls = append(calls, "inspect")
			version := "1.0.0"
			if len(calls) > 2 {
				version = "1.2.3"
			}
			return project.Info{Root: root, PackageFile: packageFile, ManifestFile: manifestFile, Output: filepath.Join(root, "dist", "app.lpk"), PackageID: "cloud.lazycat.example", Version: version}, nil
		},
		SetVersion: func(filename, version string) (yamledit.Change, error) {
			calls = append(calls, "edit")
			if filename != packageFile || version != "1.2.3" {
				t.Fatalf("edit filename=%q version=%q", filename, version)
			}
			return yamledit.Change{Changed: true, Old: "1.0.0", New: "1.2.3"}, nil
		},
		Build: func(_ context.Context, request actionbuild.Request) (actionbuild.Result, error) {
			calls = append(calls, "build")
			if request.Version != "1.2.3" || request.Project.Version != "1.2.3" || request.Project.Output == "" {
				t.Fatalf("request=%#v", request)
			}
			return actionbuild.Result{Path: request.Project.Output, PackageID: request.Project.PackageID, Version: request.Version, SHA256: strings.Repeat("a", 64), TargetPlatform: "linux/amd64"}, nil
		},
	}

	result, err := action.Run(context.Background(), action.Input{
		Operation: action.OperationBuild, ConfigPath: ".github/lazycat-action.yml", Version: "1.2.3", Tag: "v1.2.3",
	}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"load", "inspect", "edit", "inspect", "build"}) {
		t.Fatalf("calls=%v", calls)
	}
	if result.Operation != "build" || !result.Changed || result.PackageID != "cloud.lazycat.example" || result.Version != "1.2.3" || result.Tag != "v1.2.3" {
		t.Fatalf("result=%#v", result)
	}
	if result.PackageFile != packageFile || result.ManifestFile != manifestFile {
		t.Fatalf("managed files=%#v", result)
	}
	if result.RunnerArch != "arm64" || result.TargetPlatform != "linux/amd64" || string(result.ImageResults) != "[]" {
		t.Fatalf("architectures/result=%#v", result)
	}
	if result.OfficialStoreEnabled || result.PrivateStoreEnabled || string(result.StoreResults) != "{}" {
		t.Fatalf("store result=%#v", result)
	}
	if result.ResultFile == "" {
		t.Fatal("result file is empty")
	}
	if _, err := os.Stat(result.ResultFile); err != nil {
		t.Fatal(err)
	}
}

func TestRunPublishesOfficialStoreAndReturnsStableJSON(t *testing.T) {
	root := t.TempDir()
	cfg := gitConfig()
	cfg.Update.Strategy = config.StrategyPublish
	cfg.Stores.Official.Enabled = true
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		ResultDir:  filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{Root: root, PackageID: "cloud.lazycat.example", Version: "1.2.3", Name: "Example"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
		Publish: func(_ context.Context, request publishflow.Request) (publishflow.Result, error) {
			if request.Target != publishflow.TargetOfficial || request.TokenFile != "/run/secrets/lazycat.json" || request.LPKPath != filepath.Join(root, "dist", "app.lpk") {
				t.Fatalf("request=%#v", request)
			}
			return publishflow.Result{
				Artifact: lpkcheckResult(filepath.Join(root, "dist", "app.lpk")),
				Official: &official.Result{Published: false, Skipped: true, OnlineVersion: "1.2.3", PackageID: "cloud.lazycat.example", Version: "1.2.3", SHA256: strings.Repeat("a", 64)},
			}, nil
		},
	}
	result, err := action.Run(context.Background(), action.Input{
		Operation: action.OperationPublishOfficial, Version: "1.2.3", LPKPath: filepath.Join(root, "dist", "app.lpk"),
		Changelog: "Release notes", TokenFile: "/run/secrets/lazycat.json",
	}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if result.Operation != "publish-official" || result.SHA256 != strings.Repeat("a", 64) || !result.OfficialStoreEnabled || string(result.StoreResults) == "{}" || !strings.Contains(string(result.StoreResults), `"skipped":true`) || !strings.Contains(string(result.StoreResults), `"onlineVersion":"1.2.3"`) {
		t.Fatalf("result=%#v", result)
	}
}

func TestRunMapsStoreAuthenticationFailure(t *testing.T) {
	cfg := gitConfig()
	cfg.Update.Strategy = config.StrategyPublish
	cfg.Stores.Official.Enabled = true
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{Root: t.TempDir(), PackageID: "cloud.lazycat.example", Version: "1.2.3"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
		Publish: func(context.Context, publishflow.Request) (publishflow.Result, error) {
			return publishflow.Result{}, &lpkgo.Error{Code: lpkgo.CodeUnauthenticated, Retryable: false}
		},
	}
	_, err := action.Run(context.Background(), action.Input{Operation: action.OperationPublishOfficial, Version: "1.2.3", LPKPath: "dist/app.lpk"}, deps)
	var actionErr *action.Error
	if !errors.As(err, &actionErr) || actionErr.Code != action.CodeStoreAuthFailed || actionErr.Retryable {
		t.Fatalf("err=%#v", err)
	}
}

func TestErrorIncludesSafeToolkitDiagnosticsWithoutCauseText(t *testing.T) {
	err := &action.Error{
		Code:    action.CodeStorePublishFailed,
		Message: "store publishing failed",
		Cause: &lpkgo.Error{
			Code: lpkgo.CodeConflict, Op: "store.private", StatusCode: 409,
			Cause: errors.New("response contains lcst_must_not_leak"),
		},
	}
	message := err.Error()
	for _, expected := range []string{"STORE_PUBLISH_FAILED", "CONFLICT", "status=409", "op=store.private"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message=%q missing %q", message, expected)
		}
	}
	if strings.Contains(message, "lcst_must_not_leak") {
		t.Fatalf("message leaked cause text: %q", message)
	}
}

func lpkcheckResult(path string) lpkcheck.Result {
	return lpkcheck.Result{Path: path, PackageID: "cloud.lazycat.example", Version: "1.2.3", SHA256: strings.Repeat("a", 64), TargetPlatform: "linux/amd64"}
}

func TestRunDryRunSkipsEditsAndBuild(t *testing.T) {
	root := t.TempDir()
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		ResultDir:  filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return gitConfig(), nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{Root: root, PackageID: "cloud.lazycat.example", Version: "1.0.0"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) {
			t.Fatal("SetVersion called during dry-run")
			return yamledit.Change{}, nil
		},
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			t.Fatal("Build called during dry-run")
			return actionbuild.Result{}, nil
		},
	}
	result, err := action.Run(context.Background(), action.Input{Operation: action.OperationBuild, Version: "1.2.3", Tag: "v1.2.3", DryRun: true}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.LPKPath != "" || result.TargetPlatform != "linux/amd64" {
		t.Fatalf("result=%#v", result)
	}
}

func TestRunRejectsSymlinkedResultDirectory(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	resultDir := filepath.Join(root, "results")
	if err := os.Symlink(external, resultDir); err != nil {
		t.Fatal(err)
	}
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		ResultDir:  resultDir,
		LoadConfig: func(string) (config.Config, error) { return gitConfig(), nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{Root: root, PackageID: "cloud.lazycat.example", Version: "1.0.0"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
	}
	_, err := action.Run(context.Background(), action.Input{Operation: action.OperationBuild, Version: "1.2.3", DryRun: true}, deps)
	if err == nil || !strings.Contains(err.Error(), action.CodeBuildFailed) {
		t.Fatalf("err=%v", err)
	}
}

func TestRunRejectsWorkflowToolchainMismatchBeforeProjectInspection(t *testing.T) {
	inspected := false
	cfg := gitConfig()
	cfg.Build.Toolchains = []config.Toolchain{{Kind: "go", Version: "1.25.x"}, {Kind: "docker"}}
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			inspected = true
			return project.Info{}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
	}
	_, err := action.Run(context.Background(), action.Input{Operation: action.OperationBuild, WorkflowToolchains: "go,node", WorkflowGoVersion: "1.24.x"}, deps)
	if err == nil || !strings.Contains(err.Error(), "workflow toolchains do not match") {
		t.Fatalf("err=%v", err)
	}
	if inspected {
		t.Fatal("project inspection ran after toolchain mismatch")
	}
}

func TestRunBuildFailureRollsBackVersion(t *testing.T) {
	root := t.TempDir()
	var edits []string
	inspectCount := 0
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		ResultDir:  filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return gitConfig(), nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			inspectCount++
			version := "1.0.0"
			if inspectCount > 1 {
				version = "1.2.3"
			}
			return project.Info{Root: root, PackageFile: filepath.Join(root, "package.yml"), PackageID: "cloud.lazycat.example", Version: version}, nil
		},
		SetVersion: func(_ string, version string) (yamledit.Change, error) {
			edits = append(edits, version)
			return yamledit.Change{Changed: true, Old: "1.0.0", New: version}, nil
		},
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, errors.New("compile failed")
		},
	}
	result, err := action.Run(context.Background(), action.Input{Operation: action.OperationBuild, Version: "1.2.3"}, deps)
	if err == nil || !strings.Contains(err.Error(), action.CodeBuildFailed) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(edits, []string{"1.2.3", "1.0.0"}) {
		t.Fatalf("edits=%v", edits)
	}
}

func TestRunRejectsNonAMD64BuildResult(t *testing.T) {
	root := t.TempDir()
	inspectCount := 0
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "arm64"},
		ResultDir:  filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return gitConfig(), nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			inspectCount++
			version := "1.0.0"
			if inspectCount > 1 {
				version = "1.2.3"
			}
			return project.Info{Root: root, PackageFile: filepath.Join(root, "package.yml"), PackageID: "cloud.lazycat.example", Version: version}, nil
		},
		SetVersion: func(_ string, version string) (yamledit.Change, error) {
			return yamledit.Change{Changed: true, Old: "1.0.0", New: version}, nil
		},
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{PackageID: "cloud.lazycat.example", Version: "1.2.3", TargetPlatform: "linux/arm64"}, nil
		},
	}
	_, err := action.Run(context.Background(), action.Input{Operation: action.OperationBuild, Version: "1.2.3"}, deps)
	if err == nil || !strings.Contains(err.Error(), action.CodeLPKInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveOperation(t *testing.T) {
	tests := []struct {
		name  string
		input action.Input
		want  action.Operation
	}{
		{name: "release", input: action.Input{Operation: action.OperationAuto, EventName: "release"}, want: action.OperationBuild},
		{name: "tag", input: action.Input{Operation: action.OperationAuto, EventName: "push", RefType: "tag", RefName: "v1.2.3"}, want: action.OperationBuild},
		{name: "manual image check", input: action.Input{Operation: action.OperationAuto, EventName: "workflow_dispatch"}, want: action.OperationCheck},
		{name: "manual version build", input: action.Input{Operation: action.OperationAuto, EventName: "workflow_dispatch", Version: "1.2.3"}, want: action.OperationBuild},
		{name: "schedule", input: action.Input{Operation: action.OperationAuto, EventName: "schedule"}, want: action.OperationCheck},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := action.ResolveOperation(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("operation=%q want=%q", got, test.want)
			}
		})
	}
}

func TestRunCheckUpdatesVersionBuildsAndReturnsImageResults(t *testing.T) {
	root := t.TempDir()
	packageFile := filepath.Join(root, "package.yml")
	inspectCount := 0
	var built actionbuild.Request
	deps := action.Dependencies{
		Host:      platform.Host{OS: "linux", Arch: "arm64"},
		ResultDir: filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) {
			cfg := gitConfig()
			cfg.Update.Strategy = config.StrategyPull
			cfg.Update.VersionSource = config.VersionSource{Type: config.VersionSourceImage, Image: "web"}
			cfg.Images = []config.Image{{ID: "web", Target: "service", Service: "web", Source: "ghcr.io/acme/web", Channel: "stable", Sort: "semver", Delivery: config.Delivery{Mode: "direct"}}}
			return cfg, nil
		},
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			inspectCount++
			version := "1.0.0"
			if inspectCount > 1 {
				version = "2.0.0"
			}
			return project.Info{Root: root, PackageFile: packageFile, ManifestFile: filepath.Join(root, "manifest.yml"), Output: filepath.Join(root, "dist", "app.lpk"), PackageID: "cloud.lazycat.example", Version: version}, nil
		},
		SetVersion: func(_ string, version string) (yamledit.Change, error) {
			return yamledit.Change{Changed: true, Old: "1.0.0", New: version}, nil
		},
		Build: func(_ context.Context, request actionbuild.Request) (actionbuild.Result, error) {
			built = request
			return actionbuild.Result{Path: request.Project.Output, PackageID: request.Project.PackageID, Version: request.Version, SHA256: strings.Repeat("a", 64), TargetPlatform: "linux/amd64"}, nil
		},
		CheckImages: func(context.Context, imageflow.Request) (imageflow.Result, error) {
			return imageflow.Result{Changed: true, Version: "2.0.0", Channel: "stable", Images: []imageflow.ImageResult{{ID: "web", Target: "service", Service: "web", Platform: "linux/amd64", SourceRef: "ghcr.io/acme/web:v2.0.0", SourceDigest: strings.Repeat("a", 64), DeliveryMode: "direct", DeliveredRef: "ghcr.io/acme/web:v2.0.0"}}}, nil
		},
	}
	result, err := action.Run(context.Background(), action.Input{Operation: action.OperationCheck}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if result.Operation != "check" || !result.Changed || result.Version != "2.0.0" || result.Tag != "v2.0.0" || result.UpdateStrategy != "pull" || result.Channel != "stable" {
		t.Fatalf("result=%#v", result)
	}
	if built.Version != "2.0.0" || built.Channel != "stable" || !strings.Contains(string(result.ImageResults), `"id":"web"`) {
		t.Fatalf("built=%#v images=%s", built, result.ImageResults)
	}
}

func TestRunCheckRejectsDirectPublishForNonVersionSourceImage(t *testing.T) {
	checkCalled := false
	cfg := gitConfig()
	cfg.Update.Strategy = config.StrategyPublish
	cfg.Update.VersionSource = config.VersionSource{Type: config.VersionSourceImage, Image: "web"}
	cfg.Images = []config.Image{
		{ID: "db", Target: "service", Service: "db", Source: "postgres", Channel: "stable", Sort: "semver", Delivery: config.Delivery{Mode: "lazycat"}},
		{ID: "web", Target: "service", Service: "web", Source: "example/web", Channel: "stable", Sort: "semver", Delivery: config.Delivery{Mode: "lazycat"}},
	}
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{PackageID: "cloud.lazycat.example", Version: "1.0.0"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
		CheckImages: func(context.Context, imageflow.Request) (imageflow.Result, error) {
			checkCalled = true
			return imageflow.Result{}, nil
		},
	}
	_, err := action.Run(context.Background(), action.Input{Operation: action.OperationCheck, ImageID: "db"}, deps)
	if err == nil || !strings.Contains(err.Error(), "must select version-source image") {
		t.Fatalf("err=%v", err)
	}
	if checkCalled {
		t.Fatal("image check ran for invalid direct-publish selection")
	}
}

func TestRunMapsImageVersionDowngrade(t *testing.T) {
	cfg := gitConfig()
	cfg.Update.VersionSource = config.VersionSource{Type: config.VersionSourceImage, Image: "web"}
	cfg.Images = []config.Image{{ID: "web", Target: "service", Service: "web", Source: "ghcr.io/acme/web", Channel: "stable", Sort: "semver", Delivery: config.Delivery{Mode: "direct"}}}
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		LoadConfig: func(string) (config.Config, error) { return cfg, nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{PackageID: "cloud.lazycat.example", Version: "19.0.0"}, nil
		},
		SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
		Build: func(context.Context, actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{}, nil
		},
		CheckImages: func(context.Context, imageflow.Request) (imageflow.Result, error) {
			return imageflow.Result{}, imageflow.ErrVersionDowngrade
		},
	}
	_, err := action.Run(context.Background(), action.Input{Operation: action.OperationCheck}, deps)
	var actionErr *action.Error
	if !errors.As(err, &actionErr) || actionErr.Code != action.CodeVersionDowngradeBlocked {
		t.Fatalf("err=%#v", err)
	}
}

func TestRunBuildUsesCurrentPackageVersionWhenInputIsEmpty(t *testing.T) {
	root := t.TempDir()
	deps := action.Dependencies{
		Host:       platform.Host{OS: "linux", Arch: "amd64"},
		ResultDir:  filepath.Join(root, "results"),
		LoadConfig: func(string) (config.Config, error) { return gitConfig(), nil },
		Inspect: func(context.Context, config.Project) (project.Info, error) {
			return project.Info{Root: root, PackageFile: filepath.Join(root, "package.yml"), Output: filepath.Join(root, "dist", "app.lpk"), PackageID: "cloud.lazycat.example", Version: "1.2.3"}, nil
		},
		SetVersion: func(_ string, version string) (yamledit.Change, error) {
			if version != "1.2.3" {
				t.Fatalf("version=%q", version)
			}
			return yamledit.Change{Old: version, New: version}, nil
		},
		Build: func(_ context.Context, request actionbuild.Request) (actionbuild.Result, error) {
			return actionbuild.Result{Path: request.Project.Output, PackageID: request.Project.PackageID, Version: request.Version, SHA256: strings.Repeat("a", 64), TargetPlatform: "linux/amd64"}, nil
		},
	}
	result, err := action.Run(context.Background(), action.Input{Operation: action.OperationBuild}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "1.2.3" || result.Tag != "v1.2.3" {
		t.Fatalf("result=%#v", result)
	}
}

func gitConfig() config.Config {
	enabled := true
	return config.Config{
		Version: 1,
		Project: config.Project{Root: ".", BuildConfig: "lzc-build.yml", PackageFile: "package.yml", Output: "dist/app.lpk"},
		Update:  config.Update{Strategy: config.StrategyPull, VersionSource: config.VersionSource{Type: config.VersionSourceGit}},
		Build:   config.Build{RunBuildScript: &enabled},
	}
}
