package build_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	actionbuild "github.com/ca-x/lazycat-github-action/internal/build"
	"github.com/ca-x/lazycat-github-action/internal/project"
	toolkitbuild "github.com/lib-x/lzc-toolkit-go/build"
	"github.com/lib-x/lzc-toolkit-go/lpk"
)

func TestBuilderBuildsVerifiesAndHashesLPKForLinuxAMD64(t *testing.T) {
	for key, value := range map[string]string{
		"LAZYCAT_TOKEN":         "developer-secret",
		"LAZYCAT_USERNAME":      "developer@example.com",
		"LAZYCAT_PASSWORD":      "account-secret",
		"LZC_CLI_TOKEN":         "fallback-secret",
		"APPSTORE_TOKEN":        "store-secret",
		"APPSTORE_URL":          "https://store.example.com",
		"APP_ID":                "42",
		"INPUT_TOKEN_FILE":      "/run/secrets/lazycat.json",
		"INPUT_SHA256":          strings.Repeat("a", 64),
		"REGISTRY_USERNAME":     "registry-user",
		"REGISTRY_PASSWORD":     "registry-secret",
		"GITHUB_TOKEN":          "github-secret",
		"GITHUB_OUTPUT":         "/tmp/untrusted-output",
		"GITHUB_STEP_SUMMARY":   "/tmp/untrusted-summary",
		"ACTIONS_RUNTIME_TOKEN": "runtime-secret",
		"ACTIONS_RESULTS_URL":   "https://results.invalid",
		"UNRELATED_BUILD_VALUE": "available",
	} {
		t.Setenv(key, value)
	}
	info := fixtureProject(t)
	runner := &recordingRunner{}
	result, err := (actionbuild.Builder{}).Build(context.Background(), actionbuild.Request{
		Project:         info,
		Version:         "1.2.3",
		Tag:             "v1.2.3",
		SourceDateEpoch: 1783641600,
		RunBuildScript:  true,
		Runner:          runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.PackageID != "cloud.lazycat.action.fixture" || result.Version != "1.2.3" {
		t.Fatalf("result=%#v", result)
	}
	if len(result.SHA256) != 64 || result.TargetPlatform != "linux/amd64" {
		t.Fatalf("result=%#v", result)
	}
	if result.Path != info.Output {
		t.Fatalf("path=%q want=%q", result.Path, info.Output)
	}
	wantEnv := map[string]string{
		"LAZYCAT_VERSION":         "1.2.3",
		"LAZYCAT_TAG":             "v1.2.3",
		"LAZYCAT_CHANNEL":         "",
		"LAZYCAT_TARGET_OS":       "linux",
		"LAZYCAT_TARGET_ARCH":     "amd64",
		"LAZYCAT_TARGET_PLATFORM": "linux/amd64",
		"SOURCE_DATE_EPOCH":       "1783641600",
	}
	for key, want := range wantEnv {
		if runner.command.Env[key] != want {
			t.Fatalf("env[%s]=%q want=%q", key, runner.command.Env[key], want)
		}
	}
	for _, key := range []string{"LAZYCAT_TOKEN", "LAZYCAT_USERNAME", "LAZYCAT_PASSWORD", "LZC_CLI_TOKEN", "APPSTORE_TOKEN", "APPSTORE_URL", "APP_ID", "INPUT_TOKEN_FILE", "INPUT_SHA256", "REGISTRY_USERNAME", "REGISTRY_PASSWORD", "GITHUB_TOKEN", "GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY", "ACTIONS_RUNTIME_TOKEN", "ACTIONS_RESULTS_URL"} {
		if _, found := runner.command.Env[key]; found {
			t.Fatalf("protected environment %s reached buildscript", key)
		}
	}
	if runner.command.Env["UNRELATED_BUILD_VALUE"] != "available" {
		t.Fatalf("ordinary build environment was not preserved: %#v", runner.command.Env)
	}

	reader, err := lpk.OpenFile(context.Background(), result.Path)
	if err != nil {
		t.Fatal(err)
	}
	effective, effectiveErr := reader.EffectiveManifest(context.Background())
	closeErr := reader.Close()
	if effectiveErr != nil || closeErr != nil {
		t.Fatal(errors.Join(effectiveErr, closeErr))
	}
	if effective.Manifest.Version != "1.2.3" {
		t.Fatalf("version=%q", effective.Manifest.Version)
	}
}

func TestBuilderOfficialWarningsCanFailTheBuild(t *testing.T) {
	info := fixtureProject(t)
	_, err := (actionbuild.Builder{}).Build(context.Background(), actionbuild.Request{
		Project: info, Version: "1.2.3", Tag: "v1.2.3", Official: true, FailOnWarnings: true,
	})
	if err == nil || !strings.Contains(err.Error(), "official lint") {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(info.Output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("final output should not exist: %v", statErr)
	}
}

func TestBuilderBasicProfileDoesNotFailForOfficialOnlyWarnings(t *testing.T) {
	info := fixtureProject(t)
	_, err := (actionbuild.Builder{}).Build(context.Background(), actionbuild.Request{
		Project: info, Version: "1.2.3", Tag: "v1.2.3", FailOnWarnings: true,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBuilderRejectsMismatchedPackageVersion(t *testing.T) {
	info := fixtureProject(t)
	_, err := (actionbuild.Builder{}).Build(context.Background(), actionbuild.Request{
		Project: info, Version: "1.2.4", Tag: "v1.2.4",
	})
	if err == nil || !strings.Contains(err.Error(), "does not match requested version") {
		t.Fatalf("err=%v", err)
	}
}

func TestBuilderRemovesTemporaryOutputAfterRunnerFailure(t *testing.T) {
	info := fixtureProject(t)
	_, err := (actionbuild.Builder{}).Build(context.Background(), actionbuild.Request{
		Project: info, Version: "1.2.3", Tag: "v1.2.3", RunBuildScript: true,
		Runner: failingRunner{err: errors.New("runner failed")},
	})
	if err == nil || !strings.Contains(err.Error(), "runner failed") {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(info.Output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("final output should not exist: %v", statErr)
	}
}

type recordingRunner struct {
	command toolkitbuild.Command
}

func (runner *recordingRunner) Run(_ context.Context, command toolkitbuild.Command) error {
	runner.command = command
	return nil
}

type failingRunner struct {
	err error
}

func (runner failingRunner) Run(context.Context, toolkitbuild.Command) error { return runner.err }

func fixtureProject(t *testing.T) project.Info {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "static-app"))
	if err != nil {
		t.Fatal(err)
	}
	return project.Info{
		Root:         root,
		BuildConfig:  filepath.Join(root, "lzc-build.yml"),
		PackageFile:  filepath.Join(root, "package.yml"),
		ManifestFile: filepath.Join(root, "lzc-manifest.yml"),
		Output:       filepath.Join(t.TempDir(), "dist", "app.lpk"),
		PackageID:    "cloud.lazycat.action.fixture",
		Version:      "1.2.3",
		Kind:         project.KindStatic,
	}
}
