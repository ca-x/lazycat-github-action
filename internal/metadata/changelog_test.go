package metadata

import (
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestWorkflowSharesResolvedChangelogBetweenReleaseAndStores(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "lazycat.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On struct {
			Call struct {
				Inputs  map[string]any `yaml:"inputs"`
				Outputs map[string]struct {
					Value string `yaml:"value"`
				} `yaml:"outputs"`
			} `yaml:"workflow_call"`
		} `yaml:"on"`
		Jobs map[string]struct {
			Outputs map[string]string `yaml:"outputs"`
			Steps   []struct {
				ID   string            `yaml:"id"`
				Uses string            `yaml:"uses"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	if _, ok := workflow.On.Call.Inputs["changelog-template"]; !ok {
		t.Fatal("missing changelog-template input")
	}
	job := workflow.Jobs["lazycat"]
	if job.Outputs["changelog"] != "${{ steps.lazycat.outputs.changelog }}" || workflow.On.Call.Outputs["changelog"].Value != "${{ jobs.lazycat.outputs.changelog }}" {
		t.Fatal("resolved changelog is not exposed")
	}
	checked := map[string]bool{}
	for _, step := range job.Steps {
		switch step.ID {
		case "lazycat":
			if step.With["changelog-template"] != "${{ inputs.changelog-template }}" {
				t.Fatal("template input is not forwarded")
			}
		case "recovery-build", "publish-private", "publish-official":
			checked[step.ID] = true
			if step.With["changelog"] != "${{ steps.lazycat.outputs.changelog }}" {
				t.Fatalf("%s uses inconsistent changelog", step.ID)
			}
		}
		if step.Uses == "softprops/action-gh-release@v3" {
			checked["release"] = true
			if step.With["body"] != "${{ steps.lazycat.outputs.changelog }}" {
				t.Fatal("GitHub Release uses inconsistent body")
			}
		}
	}
	if len(checked) != 4 {
		t.Fatalf("missing publication paths: %v", checked)
	}
}
