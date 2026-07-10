package metadata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestActionAndReleaseMetadataAreValidYAML(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, name := range []string{"action.yml", ".goreleaser.yml", ".github/workflows/ci.yml", ".github/workflows/lazycat.yml", ".github/workflows/release.yml"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := yaml.Unmarshal(data, &document); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if len(document) == 0 {
			t.Fatalf("%s is empty", name)
		}
	}
}

func TestReusableWorkflowContractAndActionPins(t *testing.T) {
	filename := filepath.Join("..", "..", ".github", "workflows", "lazycat.yml")
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	on, ok := document["on"].(map[string]any)
	if !ok {
		t.Fatalf("workflow on=%#v", document["on"])
	}
	call, ok := on["workflow_call"].(map[string]any)
	if !ok {
		t.Fatalf("workflow_call=%#v", on["workflow_call"])
	}
	inputs, _ := call["inputs"].(map[string]any)
	for _, name := range []string{"config", "operation", "runner", "image-id", "dry-run", "changelog", "toolchains", "go-version", "node-version", "rust-toolchain", "node-package-manager", "enable-qemu"} {
		if _, found := inputs[name]; !found {
			t.Fatalf("missing workflow input %q", name)
		}
	}
	secrets, _ := call["secrets"].(map[string]any)
	for _, name := range []string{"LAZYCAT_TOKEN", "LZC_CLI_TOKEN", "REGISTRY", "REGISTRY_USERNAME", "REGISTRY_PASSWORD"} {
		if _, found := secrets[name]; !found {
			t.Fatalf("missing workflow secret %q", name)
		}
	}
	outputs, _ := call["outputs"].(map[string]any)
	for _, name := range []string{"operation", "changed", "package-id", "package-file", "manifest-file", "version", "tag", "lpk-path", "sha256", "download-url", "image-results", "update-strategy", "channel", "result-file", "runner-arch", "target-platform"} {
		if _, found := outputs[name]; !found {
			t.Fatalf("missing workflow output %q", name)
		}
	}
	permissions, _ := document["permissions"].(map[string]any)
	for _, name := range []string{"contents", "pull-requests"} {
		if got := permissions[name]; got != "write" {
			t.Fatalf("workflow permission %q=%#v, want write", name, got)
		}
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "uses: ") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "uses: "))
		value = strings.Fields(value)[0]
		if value == "ca-x/lazycat-github-action@v1" {
			continue
		}
		parts := strings.Split(value, "@")
		if len(parts) != 2 || len(parts[1]) != 40 {
			t.Fatalf("third-party Action is not pinned to a commit SHA: %s", value)
		}
	}
	workflow := string(data)
	for _, condition := range []string{
		"steps.lazycat.outputs.operation == 'check' && steps.lazycat.outputs.changed == 'true' && steps.lazycat.outputs.update-strategy == 'pull'",
		"steps.lazycat.outputs.operation == 'check' && steps.lazycat.outputs.changed == 'true' && steps.lazycat.outputs.update-strategy == 'publish'",
	} {
		if !strings.Contains(workflow, condition) {
			t.Fatalf("workflow is missing operation-based update condition %q", condition)
		}
	}
	if strings.Contains(workflow, "outputs.channel != ''") {
		t.Fatal("workflow must not use channel presence to classify check operations")
	}
	managedPaths := "add-paths: |\n            ${{ steps.lazycat.outputs.package-file }}\n            ${{ steps.lazycat.outputs.manifest-file }}"
	if !strings.Contains(workflow, managedPaths) {
		t.Fatal("workflow PR does not restrict changes to the managed package and Manifest paths")
	}
}

func TestActionMetadataExposesStableContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Inputs  map[string]any `yaml:"inputs"`
		Outputs map[string]any `yaml:"outputs"`
		Runs    struct {
			Using string `yaml:"using"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"operation", "config", "image-id", "version", "changelog", "lpk-path", "download-url", "token-file", "dry-run"} {
		if _, exists := document.Inputs[input]; !exists {
			t.Fatalf("missing input %q", input)
		}
	}
	for _, output := range []string{"operation", "changed", "package-id", "package-file", "manifest-file", "version", "tag", "lpk-path", "sha256", "download-url", "image-results", "store-results", "official-store-enabled", "private-store-enabled", "update-strategy", "channel", "result-file", "runner-arch", "target-platform"} {
		if _, exists := document.Outputs[output]; !exists {
			t.Fatalf("missing output %q", output)
		}
	}
	if document.Runs.Using != "composite" {
		t.Fatalf("runs.using=%q", document.Runs.Using)
	}
}

func TestActionMetadataUsesBracketSyntaxForHyphenatedNames(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expression := range []string{
		"steps.run.outputs['package-id']",
		"steps.run.outputs['package-file']",
		"steps.run.outputs['manifest-file']",
		"steps.run.outputs['target-platform']",
		"steps.run.outputs['update-strategy']",
		"inputs['image-id']",
		"inputs['download-url']",
		"inputs['token-file']",
	} {
		if !strings.Contains(text, expression) {
			t.Fatalf("action.yml is missing safe expression %q", expression)
		}
	}
}
