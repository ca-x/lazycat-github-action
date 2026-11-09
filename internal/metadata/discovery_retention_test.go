package metadata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/config"
)

func TestAutomaticUpstreamTagDiscoveryDocumentation(testContext *testing.T) {
	for _, filename := range []string{
		"README.md", "README.zh-CN.md", "skills/lazycat-github-action/SKILL.md", "skills/lazycat-github-action/references/configuration.md",
	} {
		data, err := os.ReadFile(filepath.Join("..", "..", filename))
		if err != nil {
			testContext.Fatal(err)
		}
		text := string(data)
		for _, required := range []string{"max_tags", "max_matching_tags", "`0`", "32 MiB", "50000", "last_updated"} {
			if !strings.Contains(text, required) {
				testContext.Fatalf("%s missing automatic discovery contract %q", filename, required)
			}
		}
		for _, stale := range []string{"defaults to `10000`", "default `10000`", "默认 `10000`", "默认也是 `10000`"} {
			if strings.Contains(text, stale) {
				testContext.Fatalf("%s still describes a default count limit: %s", filename, stale)
			}
		}
	}
}

func TestStarterReleaseCleanupDefaults(testContext *testing.T) {
	cfg, err := config.Load(filepath.Join("..", "..", "skills", "lazycat-github-action", "assets", "lazycat-action.yml"))
	if err != nil {
		testContext.Fatal(err)
	}
	if !cfg.Release.Cleanup.ShouldCleanup() || cfg.Release.Cleanup.KeepLatestCount() != 1000 {
		testContext.Fatalf("cleanup=%+v", cfg.Release.Cleanup)
	}
}
