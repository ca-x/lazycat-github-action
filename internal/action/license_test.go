package action_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/action"
	actionbuild "github.com/ca-x/lazycat-github-action/internal/build"
	"github.com/ca-x/lazycat-github-action/internal/config"
	"github.com/ca-x/lazycat-github-action/internal/imageflow"
	"github.com/ca-x/lazycat-github-action/internal/platform"
	"github.com/ca-x/lazycat-github-action/internal/project"
	"github.com/ca-x/lazycat-github-action/internal/yamledit"
	"go.yaml.in/yaml/v3"
)

func TestLicenseSyncDoesNotBlockAndPreservesSeparateSnapshot(t *testing.T) {
	for _, test := range []struct {
		name         string
		operation    action.Operation
		license      string
		lookupErr    bool
		disabled     bool
		dry          bool
		imageChanged bool
		snapshotFail bool
	}{
		{name: "build synchronizes", operation: action.OperationBuild, license: "MIT"},
		{name: "license-only check", operation: action.OperationCheck, license: "MIT"},
		{name: "image and license update", operation: action.OperationCheck, license: "MIT", imageChanged: true},
		{name: "snapshot failure still builds", operation: action.OperationBuild, license: "MIT", snapshotFail: true},
		{name: "lookup failure still builds", operation: action.OperationBuild, lookupErr: true},
		{name: "lookup failure still checks", operation: action.OperationCheck, lookupErr: true, imageChanged: true},
		{name: "unknown leaves package alone", operation: action.OperationBuild},
		{name: "same license no extra change", operation: action.OperationBuild, license: "Apache-2.0"},
		{name: "disabled", operation: action.OperationBuild, license: "MIT", disabled: true},
		{name: "dry run", operation: action.OperationBuild, license: "MIT", dry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			packageFile := filepath.Join(root, "package.yml")
			original := "package: cloud.lazycat.example\nversion: 1.0.0\nlicense: Apache-2.0 # keep comment\nhomepage: https://github.com/google/licensecheck\n"
			if err := os.WriteFile(packageFile, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := gitConfig()
			cfg.Update.VersionSource.Type = config.VersionSourceImage
			if test.disabled {
				enabled := false
				cfg.Update.SyncLicense = &enabled
			}
			lookedUp, built := false, false
			inspect := func(context.Context, config.Project) (project.Info, error) {
				data, err := os.ReadFile(packageFile)
				if err != nil {
					return project.Info{}, err
				}
				var pkg struct {
					Version string `yaml:"version"`
					License string `yaml:"license"`
				}
				if err := yaml.Unmarshal(data, &pkg); err != nil {
					return project.Info{}, err
				}
				return project.Info{Root: root, PackageFile: packageFile, Version: pkg.Version, License: pkg.License, Homepage: "https://github.com/google/licensecheck", PackageID: "cloud.lazycat.example", Output: filepath.Join(root, "app.lpk")}, nil
			}
			deps := action.Dependencies{Host: platform.Host{OS: "linux", Arch: "amd64"}, LoadConfig: func(string) (config.Config, error) { return cfg, nil }, Inspect: inspect, SetVersion: yamledit.SetPackageVersion,
				LookupLicense: func(context.Context, string) (string, error) {
					lookedUp = true
					if test.lookupErr {
						return "", errors.New("API down")
					}
					return test.license, nil
				},
				CheckImages: func(context.Context, imageflow.Request) (imageflow.Result, error) {
					v := "1.0.0"
					if test.imageChanged {
						v = "1.1.0"
					}
					return imageflow.Result{Version: v, Changed: test.imageChanged}, nil
				},
				Build: func(_ context.Context, req actionbuild.Request) (actionbuild.Result, error) {
					built = true
					return actionbuild.Result{PackageID: req.Project.PackageID, Path: req.Project.Output, Version: req.Version, TargetPlatform: "linux/amd64"}, nil
				},
			}
			if test.snapshotFail {
				deps.ResultDir = filepath.Join(root, "results")
				// Obstruct snapshot creation during the lookup; release the obstruction before building.
				deps.LookupLicense = func(context.Context, string) (string, error) {
					lookedUp = true
					if err := os.Symlink(root, deps.ResultDir); err != nil {
						t.Fatal(err)
					}
					return "MIT", nil
				}
				deps.SetVersion = func(filename, value string) (yamledit.Change, error) {
					if err := os.Remove(deps.ResultDir); err != nil {
						t.Fatal(err)
					}
					return yamledit.SetPackageVersion(filename, value)
				}
			}
			input := action.Input{Operation: test.operation, DryRun: test.dry}
			if test.operation == action.OperationBuild {
				input.Version = "1.1.0"
			}
			result, err := action.Run(t.Context(), input, deps)
			if err != nil {
				t.Fatal(err)
			}
			wantSync := test.license == "MIT" && !test.lookupErr && !test.disabled && !test.dry && !test.snapshotFail
			if result.LicenseChanged != wantSync {
				t.Fatalf("result=%+v", result)
			}
			if lookedUp == (test.disabled || test.dry) {
				t.Fatalf("lookup=%t", lookedUp)
			}
			wantBuild := !test.dry && (test.operation == action.OperationBuild || test.imageChanged)
			if built != wantBuild {
				t.Fatalf("built=%t want=%t", built, wantBuild)
			}
			data, _ := os.ReadFile(packageFile)
			if wantSync {
				snapshot, err := os.ReadFile(result.LicenseFile)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(snapshot), "version: 1.0.0") || !strings.Contains(string(snapshot), "license: MIT") || !strings.Contains(string(snapshot), "# keep comment") {
					t.Fatalf("snapshot=%s", snapshot)
				}
				if !result.Changed || result.License != "MIT" || !strings.Contains(string(data), "license: MIT") {
					t.Fatalf("result=%+v package=%s", result, data)
				}
			} else if !strings.Contains(string(data), "license: Apache-2.0") {
				t.Fatalf("license overwritten: %s", data)
			}
		})
	}
}
