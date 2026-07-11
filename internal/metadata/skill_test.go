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
	for _, required := range []string{"name: lazycat-github-action", "Use when", "Do not infer", "linux/amd64", "APPSTORE_TOKEN", "token-file", "skip_if_version_exists", "PRIVATE_STORE_GROUP_CODES", "onlineVersion", "Repository overrides Organization"} {
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
	if evals.Version != 1 || len(evals.Cases) < 7 {
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
}
