package metadata

import (
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestActionAndReleaseMetadataAreValidYAML(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, name := range []string{"action.yml", ".goreleaser.yml", ".github/workflows/release.yml"} {
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
	for _, input := range []string{"operation", "config", "image-id", "version", "changelog", "lpk-path", "download-url", "dry-run"} {
		if _, exists := document.Inputs[input]; !exists {
			t.Fatalf("missing input %q", input)
		}
	}
	for _, output := range []string{"changed", "package-id", "version", "tag", "lpk-path", "sha256", "download-url", "image-results", "result-file", "runner-arch", "target-platform"} {
		if _, exists := document.Outputs[output]; !exists {
			t.Fatalf("missing output %q", output)
		}
	}
	if document.Runs.Using != "composite" {
		t.Fatalf("runs.using=%q", document.Runs.Using)
	}
}
