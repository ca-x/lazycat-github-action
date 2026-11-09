package action

import (
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/platform"
	"github.com/ca-x/lazycat-github-action/internal/project"
)

func TestReleaseCleanupResultConfiguration(testContext *testing.T) {
	disabled := false
	keep := config.RetentionCount(25)
	for _, test := range []struct {
		cleanup config.ReleaseCleanup
		enabled bool
		keep    int
	}{
		{enabled: true, keep: 1000},
		{cleanup: config.ReleaseCleanup{Enabled: &disabled, KeepLatest: &keep}, keep: 25},
	} {
		result := baseResult(Input{}, platform.Host{}, project.Info{}, config.Config{Release: config.Release{Cleanup: test.cleanup}})
		if result.ReleaseCleanupEnabled != test.enabled || result.ReleaseKeepLatest != test.keep {
			testContext.Fatalf("result=%+v", result)
		}
	}
}
