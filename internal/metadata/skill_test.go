package metadata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositorySkillContractAndEvals(t *testing.T) {
	root := filepath.Join("..", "..", "skills", "lazycat-github-action")
	for _, name := range []string{
		"SKILL.md", "agents/openai.yaml", "references/configuration.md", "references/workflows.md",
		"assets/lazycat-action.yml", "assets/lazycat-workflow.yml", "evals/evals.json",
	} {
		if info, err := os.Stat(filepath.Join(root, name)); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("skill file %q: info=%v err=%v", name, info, err)
		}
	}
	skill, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(skill)
	for _, required := range []string{"name: lazycat-github-action", "automatically inspect", "Primary outcome: working GitHub workflows", "Do not stop after printing sample YAML", "Do not infer", "linux/amd64", "APPSTORE_TOKEN", "token-file", "skip_if_version_exists", "PRIVATE_STORE_GROUP_CODES", "onlineVersion", "Repository overrides Organization"} {
		if !strings.Contains(text, required) {
			t.Fatalf("SKILL.md missing %q", required)
		}
	}
	var evals struct {
		Version int `json:"version"`
		Cases   []struct {
			ID             string   `json:"id"`
			Prompt         string   `json:"prompt"`
			MustInclude    []string `json:"mustInclude"`
			MustNotInclude []string `json:"mustNotInclude"`
		} `json:"cases"`
	}
	data, err := os.ReadFile(filepath.Join(root, "evals", "evals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &evals); err != nil {
		t.Fatal(err)
	}
	if evals.Version != 1 || len(evals.Cases) < 8 {
		t.Fatalf("eval metadata=%#v", evals)
	}
	seen := make(map[string]struct{}, len(evals.Cases))
	for _, eval := range evals.Cases {
		if eval.ID == "" || eval.Prompt == "" || len(eval.MustInclude) == 0 || len(eval.MustNotInclude) == 0 {
			t.Fatalf("incomplete eval=%#v", eval)
		}
		if _, found := seen[eval.ID]; found {
			t.Fatalf("duplicate eval id %q", eval.ID)
		}
		seen[eval.ID] = struct{}{}
	}
	if _, found := seen["dual-store-version-deduplication"]; !found {
		t.Fatal("evals are missing dual-store version deduplication coverage")
	}
	if _, found := seen["automatic-workflow-generation"]; !found {
		t.Fatal("evals are missing automatic workflow generation coverage")
	}
	for _, name := range []string{"references/configuration.md", "references/workflows.md", "assets/lazycat-action.yml"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if !strings.Contains(text, "skip_if_version_exists") {
			t.Fatalf("%s is missing skip_if_version_exists", name)
		}
	}
	workflow, err := os.ReadFile(filepath.Join(root, "references", "workflows.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workflow), "PRIVATE_STORE_GROUP_CODES") || !strings.Contains(string(workflow), "GitHub Secret") || !strings.Contains(string(workflow), "Repository overrides Organization") {
		t.Fatal("workflow reference must document private group codes as a GitHub Secret")
	}
	for _, required := range []string{
		"historical LPK migration",
		"Go Template Manifest",
		"git ls-files '*.lpk'",
		"total bytes",
		"before deleting tracked LPKs",
		"declines",
		"preserve every tracked LPK",
		"*.lpk",
		"versioned-release-asset: true",
		"<package-id>-v<version>.lpk",
		"never execute or evaluate",
		"if`, `else`, `end`, `with`, and `range",
		"fail closed",
		"Do Not",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("SKILL.md missing expanded contract %q", required)
		}
	}
	for _, name := range []string{"references/configuration.md", "references/workflows.md", "assets/lazycat-workflow.yml"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "versioned-release-asset") {
			t.Fatalf("%s is missing versioned-release-asset", name)
		}
	}
	for _, name := range []string{"references/configuration.md", "references/workflows.md"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, required := range []string{"Go Template", "never evaluat", "fail closed"} {
			if !strings.Contains(string(data), required) {
				t.Fatalf("%s is missing template contract %q", name, required)
			}
		}
	}
	prompts, err := os.ReadFile(filepath.Join(root, "test-prompts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var promptCases []struct {
		ID       string `json:"id"`
		Prompt   string `json:"prompt"`
		Expected string `json:"expected"`
	}
	if err := json.Unmarshal(prompts, &promptCases); err != nil {
		t.Fatal(err)
	}
	if len(promptCases) != 6 {
		t.Fatalf("test-prompts.json cases=%d, want 6", len(promptCases))
	}
	promptIDs := make(map[string]bool, len(promptCases))
	for _, prompt := range promptCases {
		if prompt.ID == "" || prompt.Prompt == "" || prompt.Expected == "" {
			t.Fatalf("incomplete test prompt=%#v", prompt)
		}
		promptIDs[prompt.ID] = true
	}
	for _, id := range []string{"historical-lpk-migration", "go-template-manifest-preservation"} {
		if !promptIDs[id] {
			t.Fatalf("test-prompts.json missing %q", id)
		}
	}
}
