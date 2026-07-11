package version

import "github.com/ca-x/lazycat-github-action/internal/platform"

var ActionVersion = "dev"

const (
	ToolkitVersion      = "v0.3.0"
	ReferenceCLIPackage = "@lazycatcloud/lzc-cli"
	ReferenceCLIVersion = "2.0.8"
)

type BuildInfo struct {
	ActionVersion       string `json:"actionVersion"`
	ToolkitVersion      string `json:"toolkitVersion"`
	ReferenceCLIPackage string `json:"referenceCliPackage"`
	ReferenceCLIVersion string `json:"referenceCliVersion"`
	TargetPlatform      string `json:"targetPlatform"`
}

func Info() BuildInfo {
	return BuildInfo{
		ActionVersion:       ActionVersion,
		ToolkitVersion:      ToolkitVersion,
		ReferenceCLIPackage: ReferenceCLIPackage,
		ReferenceCLIVersion: ReferenceCLIVersion,
		TargetPlatform:      platform.TargetPlatform,
	}
}
