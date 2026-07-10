package githubio_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/action"
	"github.com/ca-x/lazycat-github-action/internal/githubio"
)

func TestReadInputNormalizesTagVersionAndActionFields(t *testing.T) {
	environment := map[string]string{
		"INPUT_OPERATION":                 "build",
		"INPUT_CONFIG":                    ".github/lazycat-action.yml",
		"INPUT_IMAGE_ID":                  "web",
		"INPUT_VERSION":                   "v1.2.3",
		"INPUT_CHANGELOG":                 "Release notes",
		"INPUT_LPK_PATH":                  "dist/app.lpk",
		"INPUT_DOWNLOAD_URL":              "https://github.com/acme/app/releases/download/v1.2.3/app.lpk",
		"INPUT_DRY_RUN":                   "true",
		"GITHUB_EVENT_NAME":               "push",
		"GITHUB_REF_TYPE":                 "tag",
		"GITHUB_REF_NAME":                 "v1.2.3",
		"SOURCE_DATE_EPOCH":               "1783641600",
		"LAZYCAT_WORKFLOW_TOOLCHAINS":     "go,docker",
		"LAZYCAT_WORKFLOW_GO_VERSION":     "1.25.x",
		"LAZYCAT_WORKFLOW_NODE_VERSION":   "22.x",
		"LAZYCAT_WORKFLOW_RUST_TOOLCHAIN": "stable",
	}
	input, err := githubio.ReadInput(func(key string) string { return environment[key] })
	if err != nil {
		t.Fatal(err)
	}
	if input.Operation != action.OperationBuild || input.ConfigPath != ".github/lazycat-action.yml" || input.ImageID != "web" {
		t.Fatalf("input=%#v", input)
	}
	if input.Version != "1.2.3" || input.Tag != "v1.2.3" || !input.DryRun || input.SourceDateEpoch != 1783641600 {
		t.Fatalf("input=%#v", input)
	}
	if input.WorkflowToolchains != "go,docker" || input.WorkflowGoVersion != "1.25.x" || input.WorkflowNodeVersion != "22.x" || input.WorkflowRustToolchain != "stable" {
		t.Fatalf("workflow toolchains=%#v", input)
	}
}

func TestReadInputReadsReleaseTagFromEventFile(t *testing.T) {
	eventFile := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(eventFile, []byte(`{"release":{"tag_name":"v2.3.4"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	environment := map[string]string{"GITHUB_EVENT_NAME": "release", "GITHUB_EVENT_PATH": eventFile}
	input, err := githubio.ReadInput(func(key string) string { return environment[key] })
	if err != nil {
		t.Fatal(err)
	}
	if input.Version != "2.3.4" || input.Tag != "v2.3.4" {
		t.Fatalf("input=%#v", input)
	}
}

func TestWriteOutputsUsesStableKeysAndDoesNotLeakSecrets(t *testing.T) {
	for key, value := range map[string]string{
		"LAZYCAT_TOKEN": "lazycat-secret", "LZC_CLI_TOKEN": "cli-secret", "LAZYCAT_PASSWORD": "password-secret", "APPSTORE_TOKEN": "store-secret",
	} {
		t.Setenv(key, value)
	}
	var output bytes.Buffer
	result := action.Result{
		Operation: "check", Changed: true, PackageID: "cloud.lazycat.example", PackageFile: "/tmp/package.yml", ManifestFile: "/tmp/lzc-manifest.yml", Version: "1.2.3", Tag: "v1.2.3", LPKPath: "/tmp/app.lpk",
		SHA256: strings.Repeat("a", 64), ImageResults: []byte("[]"), UpdateStrategy: "pull", Channel: "stable", ResultFile: "/tmp/result.json", RunnerArch: "arm64", TargetPlatform: "linux/amd64",
	}
	if err := githubio.WriteOutputs(&output, result); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, key := range []string{"operation", "changed", "package-id", "package-file", "manifest-file", "version", "tag", "lpk-path", "sha256", "download-url", "image-results", "update-strategy", "channel", "result-file", "runner-arch", "target-platform"} {
		if !strings.Contains(got, key+"<<lazycat_output_") {
			t.Fatalf("missing key %q in:\n%s", key, got)
		}
	}
	for _, secret := range []string{"lazycat-secret", "cli-secret", "password-secret", "store-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("output leaked secret %q", secret)
		}
	}
}

func TestWriteOutputsChangesDelimiterWhenValueStartsWithDelimiterLine(t *testing.T) {
	var output bytes.Buffer
	result := action.Result{PackageID: "lazycat_output_2\nforged=value", ImageResults: []byte("[]")}
	if err := githubio.WriteOutputs(&output, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "package-id<<lazycat_output_2_x\n") {
		t.Fatalf("output delimiter was not changed:\n%s", output.String())
	}
}

func TestReadInputRejectsInvalidBooleanAndVersion(t *testing.T) {
	tests := []map[string]string{
		{"INPUT_DRY_RUN": "sometimes", "INPUT_VERSION": "1.2.3"},
		{"INPUT_DRY_RUN": "false", "INPUT_VERSION": "latest"},
	}
	for _, environment := range tests {
		if _, err := githubio.ReadInput(func(key string) string { return environment[key] }); err == nil {
			t.Fatalf("expected environment %#v to fail", environment)
		}
	}
}
