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
	frontmatterEnd := strings.Index(text[4:], "\n---")
	if !strings.HasPrefix(text, "---\n") || frontmatterEnd < 0 {
		t.Fatal("SKILL.md has invalid frontmatter")
	}
	frontmatter := text[:frontmatterEnd+4]
	for _, required := range []string{"historical LPK migration or cleanup", "Go Template Manifest preservation"} {
		if !strings.Contains(frontmatter, required) {
			t.Fatalf("SKILL.md frontmatter missing trigger %q", required)
		}
	}
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
	checkpoint := markdownSection(text, "## 🔴 CHECKPOINT — before deleting tracked LPKs")
	for _, required := range []string{
		"git ls-files '*.lpk'",
		"count",
		"total bytes",
		"STOP",
		"explicit yes/no answer immediately before deletion",
		"declines",
		"preserve every tracked LPK",
	} {
		if !strings.Contains(checkpoint, required) {
			t.Fatalf("historical-LPK checkpoint missing %q", required)
		}
	}
	readmes := []struct {
		name     string
		heading  string
		required []string
	}{
		{"README.md", "## Using the Skill", []string{"package.yml", "lzc-build.yml", "Manifest", ".github/lazycat-action.yml", ".github/workflows/*.yml", "pauses", "GitHub Secret"}},
		{"README.zh-CN.md", "## 使用 Skill", []string{"package.yml", "lzc-build.yml", "Manifest", ".github/lazycat-action.yml", ".github/workflows/*.yml", "暂停", "GitHub Secret"}},
	}
	for _, readme := range readmes {
		data, err := os.ReadFile(filepath.Join("..", "..", readme.name))
		if err != nil {
			t.Fatal(err)
		}
		section := markdownSection(string(data), readme.heading)
		for _, required := range readme.required {
			if !strings.Contains(section, required) {
				t.Fatalf("%s Skill usage section missing %q", readme.name, required)
			}
		}
	}
	for _, name := range []string{"references/configuration.md", "references/workflows.md"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "versioned-release-asset") {
			t.Fatalf("%s is missing versioned-release-asset", name)
		}
	}
	starter, err := os.ReadFile(filepath.Join(root, "assets", "lazycat-workflow.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(starter), "versioned-release-asset: true") {
		t.Fatal("scheduled pull workflow starter must not enable versioned-release-asset")
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
	promptIDs := make(map[string]string, len(promptCases))
	for _, prompt := range promptCases {
		if prompt.ID == "" || prompt.Prompt == "" || prompt.Expected == "" {
			t.Fatalf("incomplete test prompt=%#v", prompt)
		}
		if _, found := promptIDs[prompt.ID]; found {
			t.Fatalf("duplicate test prompt id %q", prompt.ID)
		}
		promptIDs[prompt.ID] = prompt.Expected
	}
	for id, required := range map[string][]string{
		"historical-lpk-migration":          {"git ls-files '*.lpk'", "总字节", "yes/no", "拒绝", "versioned-release-asset: true", "<package-id>-v<version>.lpk"},
		"go-template-manifest-preservation": {"绝不执行或求值", "if/else/end/with/range", "逐字节", "fail closed"},
	} {
		expected, found := promptIDs[id]
		if !found {
			t.Fatalf("test-prompts.json missing %q", id)
		}
		for _, value := range required {
			if !strings.Contains(expected, value) {
				t.Fatalf("test prompt %q expected result missing %q", id, value)
			}
		}
	}
	contractFiles := []string{"SKILL.md", "references/configuration.md", "references/workflows.md"}
	for _, name := range contractFiles {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		contract := string(data)
		for _, forbidden := range []string{"both stores receive the same", "both store consumers use the same", "both stores resolve the same", "both stores use the same"} {
			if strings.Contains(contract, forbidden) {
				t.Fatalf("%s contains inaccurate store URL contract %q", name, forbidden)
			}
		}
	}
	for _, required := range []string{"private store uses the verified GitHub Release Asset URL and SHA256", "official store uploads the same locally verified LPK bytes and SHA256 without receiving the Release URL"} {
		if !strings.Contains(text, required) {
			t.Fatalf("SKILL.md missing store asset contract %q", required)
		}
	}
}

func markdownSection(text, heading string) string {
	start := strings.Index(text, heading)
	if start < 0 {
		return ""
	}
	rest := text[start+len(heading):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		return rest[:end]
	}
	return rest
}
