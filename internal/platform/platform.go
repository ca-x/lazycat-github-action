package platform

import "fmt"

const (
	TargetOS       = "linux"
	TargetArch     = "amd64"
	TargetPlatform = TargetOS + "/" + TargetArch
)

type Host struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

func NormalizeHost(goos, goarch string) (Host, error) {
	if goos != "linux" {
		return Host{}, fmt.Errorf("unsupported Action host OS %q: only linux is supported", goos)
	}
	if goarch != "amd64" && goarch != "arm64" {
		return Host{}, fmt.Errorf("unsupported Action host architecture %q: supported values are amd64 and arm64", goarch)
	}
	return Host{OS: goos, Arch: goarch}, nil
}
