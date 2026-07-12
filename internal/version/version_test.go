package version_test

import (
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/version"
)

func TestInfoReportsToolkitAndCLICompatibility(t *testing.T) {
	info := version.Info()
	if info.ToolkitVersion != "v0.3.2" || info.ReferenceCLIVersion != "2.0.8" {
		t.Fatalf("info=%#v", info)
	}
}
