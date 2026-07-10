package platform_test

import (
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/platform"
)

func TestNormalizeHostKeepsHostSeparateFromTarget(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		goarch   string
		wantHost platform.Host
		wantErr  bool
	}{
		{name: "amd64", goos: "linux", goarch: "amd64", wantHost: platform.Host{OS: "linux", Arch: "amd64"}},
		{name: "arm64", goos: "linux", goarch: "arm64", wantHost: platform.Host{OS: "linux", Arch: "arm64"}},
		{name: "mac rejected", goos: "darwin", goarch: "arm64", wantErr: true},
		{name: "arm v7 rejected", goos: "linux", goarch: "arm", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host, err := platform.NormalizeHost(test.goos, test.goarch)
			if (err != nil) != test.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, test.wantErr)
			}
			if host != test.wantHost {
				t.Fatalf("host=%#v want=%#v", host, test.wantHost)
			}
			if platform.TargetPlatform != "linux/amd64" {
				t.Fatalf("target=%q", platform.TargetPlatform)
			}
		})
	}
}
