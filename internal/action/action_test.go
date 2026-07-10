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
	"github.com/ca-x/lazycat-github-action/internal/platform"
	"github.com/ca-x/lazycat-github-action/internal/project"
	"github.com/ca-x/lazycat-github-action/internal/yamledit"
)

func TestRunBuildCallsDependenciesInOrderAndReturnsStableResult(t *testing.T) {
	var calls []string
	root := t.TempDir()
	packageFile := filepath.Join(root, "package.yml")
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
			return project.Info{Root: root, PackageFile: packageFile, Output: filepath.Join(root, "dist", "app.lpk"), PackageID: "cloud.lazycat.example", Version: version}, nil
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
	if !result.Changed || result.PackageID != "cloud.lazycat.example" || result.Version != "1.2.3" || result.Tag != "v1.2.3" {
		t.Fatalf("result=%#v", result)
	}
	if result.RunnerArch != "arm64" || result.TargetPlatform != "linux/amd64" || string(result.ImageResults) != "[]" {
		t.Fatalf("architectures/result=%#v", result)
	}
	if result.ResultFile == "" {
		t.Fatal("result file is empty")
	}
	if _, err := os.Stat(result.ResultFile); err != nil {
		t.Fatal(err)
	}
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
		{name: "manual", input: action.Input{Operation: action.OperationAuto, EventName: "workflow_dispatch"}, want: action.OperationBuild},
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

func gitConfig() config.Config {
	enabled := true
	return config.Config{
		Version: 1,
		Project: config.Project{Root: ".", BuildConfig: "lzc-build.yml", PackageFile: "package.yml", Output: "dist/app.lpk"},
		Update:  config.Update{Strategy: config.StrategyPull, VersionSource: config.VersionSource{Type: config.VersionSourceGit}},
		Build:   config.Build{RunBuildScript: &enabled},
	}
}
