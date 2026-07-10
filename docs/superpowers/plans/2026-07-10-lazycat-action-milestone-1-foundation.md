# LazyCat Action Milestone 1 Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `executing-plans` to implement this plan task-by-task. The repository owner requires inline main-agent execution; do not dispatch subagents. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a directly usable Linux amd64/arm64 GitHub Action foundation that updates a Git-sourced package version, runs an x64-targeted project buildscript, builds and reopens an LPK with `lzc-toolkit-go`, applies optional official lint, computes SHA256, and returns stable GitHub outputs.

**Architecture:** A small `cmd/lazycat-action` process delegates to focused internal packages for platform constants, configuration, YAML editing, project inspection, LPK building, and GitHub Action I/O. The Go binary is host-architecture-specific, while every build-facing contract is fixed to `linux/amd64`. A composite `action.yml` downloads and verifies the correct host binary; GoReleaser produces both release artifacts.

**Tech Stack:** Go 1.25, `github.com/lib-x/lzc-toolkit-go v0.1.0`, `go.yaml.in/yaml/v3`, GitHub composite Actions, GoReleaser, shell tests, table-driven Go tests.

## Global Constraints

- Module path is `github.com/ca-x/lazycat-github-action`.
- Pin `github.com/lib-x/lzc-toolkit-go` to `v0.1.0`; its lzc-cli compatibility baseline is `2.0.8`.
- Support Action hosts `linux/amd64` and `linux/arm64`; reject other OS/architectures.
- LazyCat target constants are fixed to `linux`, `amd64`, and `linux/amd64`; v1 exposes no target-architecture input.
- Build scripts always receive `LAZYCAT_TARGET_OS=linux`, `LAZYCAT_TARGET_ARCH=amd64`, and `LAZYCAT_TARGET_PLATFORM=linux/amd64`.
- Default update strategy is `pull`; Milestone 1 implements Git/tag-sourced builds and leaves PR/Release orchestration to Milestone 2.
- Unknown configuration fields fail during decoding.
- Official lint is opt-in and uses `lint.WithOfficial()`; non-official builds do not receive official registry/icon/locales restrictions.
- All output files are created atomically; failed builds do not leave a final `.lpk`.
- Secrets and environment values are never serialized into errors, result JSON, or step summaries.
- Use TDD and make one focused commit per task.

---

## File Structure

```text
go.mod                         module and pinned dependencies
go.sum                         dependency checksums
cmd/lazycat-action/main.go     process entry, signals, exit code
internal/platform/platform.go  host normalization and fixed target constants
internal/platform/platform_test.go
internal/config/types.go       versioned public YAML configuration model
internal/config/load.go        strict loader, defaults, validation
internal/config/load_test.go
internal/project/project.go    project paths and manifest classification
internal/project/project_test.go
internal/yamledit/version.go   comment-preserving package version edit
internal/yamledit/version_test.go
internal/build/build.go        buildscript, toolkit build, reopen, lint, digest
internal/build/build_test.go
internal/action/action.go      operation orchestration and stable result
internal/action/action_test.go
internal/githubio/env.go       INPUT_/event parsing and secret-safe output writer
internal/githubio/env_test.go
internal/version/version.go    Action and SDK compatibility metadata
action.yml                     composite Action public contract
scripts/run-action.sh          host binary selection, download, checksum, execute
scripts/run-action_test.sh     shell-level selection and rejection tests
.goreleaser.yml                linux amd64/arm64 release artifacts
.github/workflows/ci.yml       Go, shell, cross-build verification
.github/workflows/release.yml  GoReleaser release workflow
testdata/static-app/           minimal no-service LPK fixture
README.md                      Milestone 1 quick start and host/target distinction
README.zh-CN.md                same content in Chinese with cross-links
```

---

### Task 1: Go module, platform contract, and version metadata

**Files:**

- Create: `go.mod`
- Create: `go.sum`
- Create: `internal/platform/platform.go`
- Create: `internal/platform/platform_test.go`
- Create: `internal/version/version.go`
- Create: `internal/version/version_test.go`

**Interfaces:**

- Produces: `platform.TargetOS`, `platform.TargetArch`, `platform.TargetPlatform` constants.
- Produces: `platform.NormalizeHost(goos, goarch string) (platform.Host, error)`.
- Produces: `version.Info() version.BuildInfo` for logs and `--version`.
- Consumes: no prior task interfaces.

- [ ] **Step 1: Create the milestone branch**

Run:

```bash
git switch -c milestone/1-foundation
```

Expected: HEAD moves to a new branch based on the committed plan on `main`.

- [ ] **Step 2: Write failing platform and version tests**

```go
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
```

```go
package version_test

import (
	"testing"

	"github.com/ca-x/lazycat-github-action/internal/version"
)

func TestInfoReportsToolkitAndCLICompatibility(t *testing.T) {
	info := version.Info()
	if info.ToolkitVersion != "v0.1.0" || info.ReferenceCLIVersion != "2.0.8" {
		t.Fatalf("info=%#v", info)
	}
}
```

- [ ] **Step 3: Run tests and confirm the packages do not exist**

Run:

```bash
go test ./internal/platform ./internal/version
```

Expected: FAIL because `go.mod` and the two packages do not exist.

- [ ] **Step 4: Create the module and minimal platform/version implementations**

Create `go.mod` with:

```go
module github.com/ca-x/lazycat-github-action

go 1.25.0

require (
	github.com/lib-x/lzc-toolkit-go v0.1.0
	go.yaml.in/yaml/v3 v3.0.4
)
```

Create `internal/platform/platform.go` with:

```go
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
```

Create `internal/version/version.go` with exact compatibility metadata and link-time Action version:

```go
package version

var ActionVersion = "dev"

const (
	ToolkitVersion      = "v0.1.0"
	ReferenceCLIPackage = "@lazycatcloud/lzc-cli"
	ReferenceCLIVersion = "2.0.8"
)

type BuildInfo struct {
	ActionVersion       string `json:"actionVersion"`
	ToolkitVersion      string `json:"toolkitVersion"`
	ReferenceCLIPackage string `json:"referenceCliPackage"`
	ReferenceCLIVersion string `json:"referenceCliVersion"`
}

func Info() BuildInfo {
	return BuildInfo{
		ActionVersion:       ActionVersion,
		ToolkitVersion:      ToolkitVersion,
		ReferenceCLIPackage: ReferenceCLIPackage,
		ReferenceCLIVersion: ReferenceCLIVersion,
	}
}
```

Run `go mod tidy` to generate `go.sum`.

- [ ] **Step 5: Run tests and static checks**

Run:

```bash
go test ./internal/platform ./internal/version
go vet ./internal/platform ./internal/version
```

Expected: both commands exit 0.

- [ ] **Step 6: Commit the platform contract**

```bash
git add go.mod go.sum internal/platform internal/version
git commit -m "feat: establish Action platform contract"
```

---

### Task 2: Strict versioned project configuration

**Files:**

- Create: `internal/config/types.go`
- Create: `internal/config/load.go`
- Create: `internal/config/load_test.go`

**Interfaces:**

- Consumes: `platform.TargetPlatform` to reject target overrides by omission from the schema.
- Produces: `config.Load(filename string) (config.Config, error)`.
- Produces: `config.Config` with normalized project, update, build, image, and store sections that later milestones can extend additively.

- [ ] **Step 1: Write table-driven strict-loader tests**

The test creates temporary YAML for these cases:

```go
tests := []struct {
	name    string
	yaml    string
	wantErr string
}{
	{
		name: "minimal git project",
		yaml: `version: 1
project:
  output: dist/app.lpk
update:
  version_source:
    type: git
`,
	},
	{
		name: "pull is default",
		yaml: `version: 1
project: {}
update:
  version_source:
    type: git
`,
	},
	{
		name: "unknown field",
		yaml: `version: 1
project:
  target_arch: arm64
update:
  version_source:
    type: git
`,
		wantErr: "field target_arch not found",
	},
	{
		name: "image source missing",
		yaml: `version: 1
project: {}
update:
  version_source:
    type: image
    image: web
`,
		wantErr: "version source image",
	},
}
```

Assertions for the successful minimal case must verify:

```go
if got.Project.Root != "." || got.Project.BuildConfig != "lzc-build.yml" || got.Project.PackageFile != "package.yml" {
	t.Fatalf("defaults=%#v", got.Project)
}
if got.Update.Strategy != config.StrategyPull {
	t.Fatalf("strategy=%q", got.Update.Strategy)
}
```

- [ ] **Step 2: Run the focused test and confirm failure**

Run:

```bash
go test ./internal/config -run TestLoad -v
```

Expected: FAIL because package `internal/config` does not exist.

- [ ] **Step 3: Define the configuration types**

Create enums and types with these exact public fields:

```go
type Strategy string
const (
	StrategyPull    Strategy = "pull"
	StrategyPublish Strategy = "publish"
)

type VersionSourceType string
const (
	VersionSourceGit   VersionSourceType = "git"
	VersionSourceImage VersionSourceType = "image"
)

type Config struct {
	Version int           `yaml:"version"`
	Project Project       `yaml:"project"`
	Update  Update        `yaml:"update"`
	Build   Build         `yaml:"build"`
	Images  []Image       `yaml:"images"`
	Stores  Stores        `yaml:"stores"`
}

type Project struct {
	Root         string `yaml:"root"`
	BuildConfig  string `yaml:"build_config"`
	PackageFile  string `yaml:"package_file"`
	Output       string `yaml:"output"`
}

type Update struct {
	Strategy      Strategy      `yaml:"strategy"`
	VersionSource VersionSource `yaml:"version_source"`
}

type VersionSource struct {
	Type  VersionSourceType `yaml:"type"`
	Image string            `yaml:"image"`
}

type Build struct {
	Toolchains     []Toolchain `yaml:"toolchains"`
	RunBuildScript *bool       `yaml:"run_buildscript"`
}

type Toolchain struct {
	Kind    string `yaml:"kind"`
	Version string `yaml:"version"`
}

type Image struct {
	ID              string   `yaml:"id"`
	Target          string   `yaml:"target"`
	Service         string   `yaml:"service"`
	Source          string   `yaml:"source"`
	Channel         string   `yaml:"channel"`
	Sort            string   `yaml:"sort"`
	TagRegex        string   `yaml:"tag_regex"`
	ExcludeRegex    string   `yaml:"exclude_regex"`
	VersionRegex    string   `yaml:"version_regex"`
	VersionTemplate string   `yaml:"version_template"`
	Delivery        Delivery `yaml:"delivery"`
}

type Delivery struct {
	Mode               string `yaml:"mode"`
	ImageTemplate      string `yaml:"image_template"`
	RequireDigestMatch bool   `yaml:"require_digest_match"`
}

type Stores struct {
	Official OfficialStore `yaml:"official"`
	Private  PrivateStore  `yaml:"private"`
}

type OfficialStore struct {
	Enabled         bool     `yaml:"enabled"`
	CreateIfMissing bool     `yaml:"create_if_missing"`
	Locales         []string `yaml:"changelog_locales"`
}

type PrivateStore struct {
	Enabled bool `yaml:"enabled"`
}
```

Milestone 1 accepts the future `images` section structurally but fails `version_source.type=image` with `image automation is not available in milestone 1`; Milestone 2 replaces that guard with full image validation without changing existing Git-source behavior.

- [ ] **Step 4: Implement strict decoding, defaults, and validation**

`Load` must:

```go
func Load(filename string) (Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return Config{}, fmt.Errorf("load Action config %q: %w", filename, err)
	}
	defer file.Close()

	decoder := yaml.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.KnownFields(true)
	var value Config
	if err := decoder.Decode(&value); err != nil {
		return Config{}, fmt.Errorf("decode Action config %q: %w", filename, err)
	}
	applyDefaults(&value)
	if err := validate(value); err != nil {
		return Config{}, fmt.Errorf("validate Action config %q: %w", filename, err)
	}
	return value, nil
}
```

Defaults are exactly `root=.`, `build_config=lzc-build.yml`, `package_file=package.yml`, `output=dist/application.lpk`, `strategy=pull`, and `run_buildscript=true`. Validation accepts only schema version `1`, ensures every configured path remains beneath `project.root`, rejects duplicate toolchain kinds and duplicate image IDs, requires `version_source.type=git` in Milestone 1, and rejects official+private only if later protocol requirements conflict; enabling both stores is otherwise valid.

- [ ] **Step 5: Run configuration tests**

Run:

```bash
go test ./internal/config -v
go vet ./internal/config
```

Expected: PASS and exit 0.

- [ ] **Step 6: Commit strict configuration**

```bash
git add internal/config
git commit -m "feat: add strict Action configuration"
```

---

### Task 3: Project discovery and comment-preserving version edits

**Files:**

- Create: `internal/project/project.go`
- Create: `internal/project/project_test.go`
- Create: `internal/yamledit/version.go`
- Create: `internal/yamledit/version_test.go`

**Interfaces:**

- Consumes: `config.Project`.
- Produces: `project.Inspect(ctx context.Context, cfg config.Project) (project.Info, error)`.
- Produces: `yamledit.SetPackageVersion(filename, version string) (yamledit.Change, error)`.

- [ ] **Step 1: Write project classification tests**

Fixtures cover these exact manifests:

```yaml
application:
  routes:
    - /=file:///lzcapp/pkg/content
```

```yaml
application:
  routes:
    - /=exec://8080,/lzcapp/pkg/content/app
```

```yaml
services:
  db:
    image: postgres:17
```

Assertions expect `KindStatic`, `KindExec`, and `KindService` respectively. An application with both `routes` and `services` is `KindService`; classification is descriptive and does not choose a main service.

- [ ] **Step 2: Write version edit tests**

Input:

```yaml
# package comment
package: cloud.lazycat.example
version: 1.0.0 # keep inline comment
name: Example
```

After `SetPackageVersion(path, "1.2.3")`, assert:

```yaml
# package comment
package: cloud.lazycat.example
version: 1.2.3 # keep inline comment
name: Example
```

Also assert a second identical call returns `Change{Changed:false, Old:"1.2.3", New:"1.2.3"}`, invalid SemVer is rejected, missing `version` is inserted immediately after `package`, and a write failure leaves the original file unchanged.

- [ ] **Step 3: Run focused tests and confirm failure**

Run:

```bash
go test ./internal/project ./internal/yamledit -v
```

Expected: FAIL because the packages do not exist.

- [ ] **Step 4: Implement project inspection**

Define:

```go
type Kind string
const (
	KindStatic  Kind = "static"
	KindExec    Kind = "exec"
	KindService Kind = "service"
)

type Info struct {
	Root         string
	BuildConfig  string
	PackageFile  string
	ManifestFile string
	Output       string
	PackageID    string
	Version      string
	Kind         Kind
}
```

`Inspect` uses `toolkitbuild.LoadConfig` to resolve `lzc-build.yml`, resolves the manifest name from `LoadedConfig.Config.Manifest` with default `lzc-manifest.yml`, parses package and manifest through `manifest.Parse`, and classifies routes by decoding route values to strings. It cleans and verifies all paths are below the absolute project root.

- [ ] **Step 5: Implement atomic YAML version editing**

Define:

```go
type Change struct {
	Changed bool   `json:"changed"`
	Old     string `json:"old"`
	New     string `json:"new"`
}

func SetPackageVersion(filename, value string) (Change, error)
```

The implementation parses a `yaml.Node`, locates the root mapping key `version`, changes only the scalar `Value` and scalar tag, preserves its comments, marshals with two-space indentation, writes a same-directory temporary file with the original mode, calls `Sync`, closes it, and renames it over the source. Validate `value` using a compiled SemVer regex that accepts `MAJOR.MINOR.PATCH` plus legal pre-release/build suffixes but not a leading `v`.

- [ ] **Step 6: Run project and YAML tests**

Run:

```bash
go test ./internal/project ./internal/yamledit -v
go vet ./internal/project ./internal/yamledit
```

Expected: PASS and exit 0.

- [ ] **Step 7: Commit project editing**

```bash
git add internal/project internal/yamledit
git commit -m "feat: inspect projects and update package versions"
```

---

### Task 4: x64 build environment, LPK verification, lint, and SHA256

**Files:**

- Create: `internal/build/build.go`
- Create: `internal/build/build_test.go`
- Create: `testdata/static-app/package.yml`
- Create: `testdata/static-app/lzc-build.yml`
- Create: `testdata/static-app/lzc-manifest.yml`
- Create: `testdata/static-app/content/index.html`
- Create: `testdata/static-app/scripts/build.sh`

**Interfaces:**

- Consumes: `project.Info`, `platform.Target*`, `toolkitbuild.BuildFile`, `lpk.OpenFile`, `lint.Package`.
- Produces: `(*build.Builder).Build(ctx context.Context, request build.Request) (build.Result, error)`.

- [ ] **Step 1: Create a minimal static fixture**

`package.yml`:

```yaml
package: cloud.lazycat.action.fixture
version: 1.2.3
name: Action Fixture
description: Static fixture
locales:
  zh:
    name: Action Fixture
    description: Static fixture
  en:
    name: Action Fixture
    description: Static fixture
```

`lzc-build.yml`:

```yaml
buildscript: ./scripts/build.sh
contentdir: ./content
```

`lzc-manifest.yml`:

```yaml
application:
  routes:
    - /=file:///lzcapp/pkg/content
```

`content/index.html` contains `<h1>LazyCat Action Fixture</h1>`.

`scripts/build.sh` is executable and verifies the fixed target without creating host-specific output:

```bash
#!/usr/bin/env bash
set -euo pipefail
test "${LAZYCAT_TARGET_OS}" = "linux"
test "${LAZYCAT_TARGET_ARCH}" = "amd64"
test "${LAZYCAT_TARGET_PLATFORM}" = "linux/amd64"
```

- [ ] **Step 2: Write build tests**

Define a recording `toolkitbuild.CommandRunner` and assert its command environment contains:

```go
want := map[string]string{
	"LAZYCAT_VERSION":         "1.2.3",
	"LAZYCAT_TAG":             "v1.2.3",
	"LAZYCAT_CHANNEL":         "",
	"LAZYCAT_TARGET_OS":       "linux",
	"LAZYCAT_TARGET_ARCH":     "amd64",
	"LAZYCAT_TARGET_PLATFORM": "linux/amd64",
	"SOURCE_DATE_EPOCH":       "1783641600",
}
```

Build to a temporary output and assert:

```go
if result.PackageID != "cloud.lazycat.action.fixture" || result.Version != "1.2.3" {
	t.Fatalf("result=%#v", result)
}
if len(result.SHA256) != 64 || result.TargetPlatform != "linux/amd64" {
	t.Fatalf("result=%#v", result)
}
reader, err := lpk.OpenFile(context.Background(), result.Path)
if err != nil { t.Fatal(err) }
defer reader.Close()
effective, err := reader.EffectiveManifest(context.Background())
if err != nil { t.Fatal(err) }
if effective.Manifest.Version != "1.2.3" { t.Fatalf("version=%q", effective.Manifest.Version) }
```

Additional tests assert official lint warnings become a typed failure when `FailOnWarnings=true`, non-official builds ignore official-only warnings, a mismatched requested version fails, and no final output remains after a runner error.

- [ ] **Step 3: Run focused tests and confirm failure**

Run:

```bash
go test ./internal/build -v
```

Expected: FAIL because `internal/build` does not exist.

- [ ] **Step 4: Implement the builder contract**

Define:

```go
type Request struct {
	Project         project.Info
	Version         string
	Tag             string
	Channel         string
	SourceDateEpoch int64
	Official        bool
	FailOnWarnings  bool
	RunBuildScript  bool
	Runner          toolkitbuild.CommandRunner
}

type Result struct {
	Path           string          `json:"path"`
	PackageID      string          `json:"packageId"`
	Version        string          `json:"version"`
	SHA256         string          `json:"sha256"`
	Size           int64           `json:"size"`
	TargetPlatform string          `json:"targetPlatform"`
	Warnings       []lpkgo.Warning `json:"warnings,omitempty"`
}

type Builder struct{}

func (Builder) Build(ctx context.Context, request Request) (Result, error)
```

The method builds into a same-directory temporary `.lpk`, passes the fixed target environment and `request.Runner` into `toolkitbuild.BuildFile`, opens the result through `lpk.OpenFile`, verifies effective package/version, extracts to a temporary directory, calls `lint.Package` with `lint.WithOfficial()` only when `request.Official`, computes SHA256 with `io.Copy(sha256.New(), file)`, fsyncs, and atomically renames the file to `request.Project.Output`.

- [ ] **Step 5: Run builder tests and package-wide checks**

Run:

```bash
go test ./internal/build -v
go test ./...
go vet ./...
```

Expected: all commands exit 0.

- [ ] **Step 6: Commit the LPK build pipeline**

```bash
git add internal/build testdata/static-app
git commit -m "feat: build and verify x64 LazyCat packages"
```

---

### Task 5: Action operation and GitHub input/output boundary

**Files:**

- Create: `internal/action/action.go`
- Create: `internal/action/action_test.go`
- Create: `internal/githubio/env.go`
- Create: `internal/githubio/env_test.go`
- Create: `cmd/lazycat-action/main.go`

**Interfaces:**

- Consumes: `config.Load`, `project.Inspect`, `yamledit.SetPackageVersion`, `build.Builder`.
- Produces: `action.Run(ctx context.Context, input action.Input, deps action.Dependencies) (action.Result, error)`.
- Produces: `githubio.ReadInput(getenv func(string) string) (action.Input, error)` and `githubio.WriteOutputs(io.Writer, action.Result) error`.

- [ ] **Step 1: Write Action orchestration tests**

Tests cover:

```go
input := action.Input{
	Operation: action.OperationBuild,
	ConfigPath: ".github/lazycat-action.yml",
	Version: "1.2.3",
	Tag: "v1.2.3",
}
```

Use function dependencies to record calls. Assert order is load config, inspect project, edit version, build. Assert `Result` includes changed, package ID, version, tag, LPK path, SHA256, runner architecture, and target platform. A build failure must preserve the original error and must not produce success outputs.

`DryRun=true` must call load/inspect, compare the current and requested versions, return the planned `Changed` value, and skip both version editing and LPK building.

Test `OperationAuto` resolution:

```go
tests := []struct{ event, refType, refName string; want action.Operation }{
	{event: "release", want: action.OperationBuild},
	{event: "push", refType: "tag", refName: "v1.2.3", want: action.OperationBuild},
	{event: "workflow_dispatch", want: action.OperationBuild},
}
```

Schedule without images returns a clear `PROJECT_UNSUPPORTED` error until Milestone 2 adds image checks.

- [ ] **Step 2: Write GitHub I/O tests**

Given `INPUT_OPERATION=build`, `INPUT_CONFIG=.github/lazycat-action.yml`, `INPUT_VERSION=1.2.3`, `GITHUB_REF_TYPE=tag`, and `GITHUB_REF_NAME=v1.2.3`, assert exact `action.Input` values. Verify multiline outputs use GitHub delimiter syntax and JSON is compact. Verify environment values named `LAZYCAT_TOKEN`, `LZC_CLI_TOKEN`, `LAZYCAT_PASSWORD`, `APPSTORE_TOKEN`, and `Authorization` never appear in output or errors.

- [ ] **Step 3: Run focused tests and confirm failure**

Run:

```bash
go test ./internal/action ./internal/githubio ./cmd/lazycat-action -v
```

Expected: FAIL because the packages do not exist.

- [ ] **Step 4: Implement stable operations, result, and error codes**

Define:

```go
type Operation string
const (
	OperationAuto            Operation = "auto"
	OperationCheck           Operation = "check"
	OperationBuild           Operation = "build"
	OperationPublishOfficial Operation = "publish-official"
	OperationPublishPrivate  Operation = "publish-private"
)

type Input struct {
	Operation       Operation
	ConfigPath      string
	ImageID         string
	Version         string
	Tag             string
	Channel         string
	Changelog       string
	LPKPath         string
	DownloadURL     string
	EventName       string
	RefType         string
	RefName         string
	SourceDateEpoch int64
	DryRun          bool
}

type Result struct {
	Changed        bool            `json:"changed"`
	PackageID      string          `json:"packageId"`
	Version        string          `json:"version"`
	Tag            string          `json:"tag"`
	LPKPath        string          `json:"lpkPath"`
	SHA256         string          `json:"sha256"`
	DownloadURL    string          `json:"downloadUrl,omitempty"`
	ImageResults   json.RawMessage `json:"imageResults"`
	ResultFile     string          `json:"resultFile"`
	RunnerArch     string          `json:"runnerArch"`
	TargetPlatform string          `json:"targetPlatform"`
	Warnings       []lpkgo.Warning `json:"warnings,omitempty"`
}

type Dependencies struct {
	Host       platform.Host
	LoadConfig func(string) (config.Config, error)
	Inspect    func(context.Context, config.Project) (project.Info, error)
	SetVersion func(string, string) (yamledit.Change, error)
	Build      func(context.Context, build.Request) (build.Result, error)
}

type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Cause     error  `json:"-"`
}
```

Milestone 1 initializes `ImageResults` to the JSON array `[]`, writes the complete secret-free result atomically to `.lazycat-action/result.json`, and sets `ResultFile` to its absolute path. Error codes are `CONFIG_INVALID`, `PROJECT_UNSUPPORTED`, `VERSION_NOT_FOUND`, `BUILD_FAILED`, and `LPK_INVALID`. `check`, `publish-official`, and `publish-private` parse all inputs but return `PROJECT_UNSUPPORTED` in this milestone. `Error.Error()` returns only code and safe message; `Unwrap()` returns cause for local diagnostics.

- [ ] **Step 5: Implement GitHub inputs and outputs**

Map hyphenated Action inputs to upper snake case environment names. Parse booleans with `strconv.ParseBool`. Derive a version from `INPUT_VERSION`, then a leading-`v` tag/ref name, then release tag name from the event JSON file. Normalize the public version by removing exactly one leading `v` and validating SemVer.

Write these output keys exactly:

```text
changed
package-id
version
tag
lpk-path
sha256
download-url
image-results
result-file
runner-arch
target-platform
```

- [ ] **Step 6: Implement the process entry**

`main.go` creates a signal-aware context for SIGINT/SIGTERM, supports `--version` by JSON-encoding `version.Info()`, validates the host with `runtime.GOOS/runtime.GOARCH`, reads inputs, calls `action.Run`, writes GitHub outputs, writes a concise step summary when `GITHUB_STEP_SUMMARY` exists, and exits 1 on typed errors without printing environment contents.

- [ ] **Step 7: Run operation tests and full checks**

Run:

```bash
go test ./internal/action ./internal/githubio ./cmd/lazycat-action -v
go test ./...
go vet ./...
```

Expected: all commands exit 0.

- [ ] **Step 8: Commit the Action process**

```bash
git add internal/action internal/githubio cmd/lazycat-action
git commit -m "feat: expose build operation as a GitHub Action process"
```

---

### Task 6: Composite Action bootstrap and dual-architecture releases

**Files:**

- Create: `action.yml`
- Create: `scripts/run-action.sh`
- Create: `scripts/run-action_test.sh`
- Create: `.goreleaser.yml`
- Create: `.github/workflows/release.yml`

**Interfaces:**

- Consumes: `cmd/lazycat-action` binary and all Action input environment variables.
- Produces: direct `uses: ca-x/lazycat-github-action@v1` execution on Linux X64/ARM64.

- [ ] **Step 1: Write shell bootstrap tests**

`scripts/run-action_test.sh` creates fake `curl`, `sha256sum`, and downloaded binaries in a temporary `PATH`. It executes these cases:

```text
RUNNER_OS=Linux RUNNER_ARCH=X64   -> requests lazycat-action_linux_amd64
RUNNER_OS=Linux RUNNER_ARCH=ARM64 -> requests lazycat-action_linux_arm64
RUNNER_OS=Windows                 -> exits non-zero with only Linux supported
RUNNER_ARCH=ARM                   -> exits non-zero with supported architectures
checksum mismatch                 -> exits before executing binary
LAZYCAT_ACTION_BINARY set         -> executes local binary without network
```

Each fake downloaded binary prints `host=<arch> target=linux/amd64`; assertions require the fixed target string in both successful cases.

- [ ] **Step 2: Run the shell test and confirm failure**

Run:

```bash
bash scripts/run-action_test.sh
```

Expected: FAIL because the bootstrap script does not exist.

- [ ] **Step 3: Create the composite Action contract**

`action.yml` declares all design-spec inputs, the stable outputs from Task 5, `runs.using: composite`, and one Bash step:

```yaml
runs:
  using: composite
  steps:
    - name: Run LazyCat Action
      shell: bash
      env:
        LAZYCAT_ACTION_VERSION: v1.0.0
        INPUT_OPERATION: ${{ inputs.operation }}
        INPUT_CONFIG: ${{ inputs.config }}
        INPUT_IMAGE_ID: ${{ inputs.image-id }}
        INPUT_VERSION: ${{ inputs.version }}
        INPUT_CHANGELOG: ${{ inputs.changelog }}
        INPUT_LPK_PATH: ${{ inputs.lpk-path }}
        INPUT_DOWNLOAD_URL: ${{ inputs.download-url }}
        INPUT_DRY_RUN: ${{ inputs.dry-run }}
      run: "${{ github.action_path }}/scripts/run-action.sh"
```

Milestone 1 parses but rejects image/store-only operations with `PROJECT_UNSUPPORTED`; this preserves the final public Action input surface while later milestones add behavior.

- [ ] **Step 4: Implement secure host binary selection**

`scripts/run-action.sh` uses `set -euo pipefail`, normalizes `X64|x86_64|amd64` to `amd64` and `ARM64|aarch64|arm64` to `arm64`, downloads from:

```text
https://github.com/ca-x/lazycat-github-action/releases/download/${LAZYCAT_ACTION_VERSION}/lazycat-action_linux_${arch}.tar.gz
https://github.com/ca-x/lazycat-github-action/releases/download/${LAZYCAT_ACTION_VERSION}/checksums.txt
```

It verifies the archive with:

```bash
(cd "$tmp" && grep "  $archive\$" checksums.txt | sha256sum --check --status)
```

Then extracts only the expected binary, rejects symlinks/non-regular files, chmods it, and `exec`s it. `LAZYCAT_ACTION_BINARY` is an explicit local-development override and must point to an executable regular file.

- [ ] **Step 5: Configure GoReleaser**

`.goreleaser.yml` builds `./cmd/lazycat-action` with:

```yaml
builds:
  - id: lazycat-action
    binary: lazycat-action
    env: [CGO_ENABLED=0]
    goos: [linux]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w -X github.com/ca-x/lazycat-github-action/internal/version.ActionVersion={{.Tag}}
archives:
  - id: binaries
    name_template: "lazycat-action_{{ .Os }}_{{ .Arch }}"
checksum:
  name_template: checksums.txt
```

The release workflow triggers on `v*` tags, uses `actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5` and `goreleaser/goreleaser-action@e435ccd777264be153ace6237001ef4d979d3a7a`, grants `contents: write`, and passes `GITHUB_TOKEN` only to the GoReleaser step.

- [ ] **Step 6: Run bootstrap and cross-build verification**

Run:

```bash
bash scripts/run-action_test.sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/lazycat-action-amd64 ./cmd/lazycat-action
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /tmp/lazycat-action-arm64 ./cmd/lazycat-action
file /tmp/lazycat-action-amd64 /tmp/lazycat-action-arm64
```

Expected: shell tests pass; `file` reports an x86-64 executable and an ARM aarch64 executable.

- [ ] **Step 7: Commit Action packaging**

```bash
git add action.yml scripts .goreleaser.yml .github/workflows/release.yml
git commit -m "feat: package Action for amd64 and arm64 runners"
```

---

### Task 7: CI, bilingual quick start, and Milestone 1 release gate

**Files:**

- Create: `.github/workflows/ci.yml`
- Create: `README.md`
- Create: `README.zh-CN.md`
- Modify: `.gitignore`

**Interfaces:**

- Consumes: all Milestone 1 commands and public Action inputs/outputs.
- Produces: reproducible CI and user-facing examples for Git/tag-sourced static and Exec builds.

- [ ] **Step 1: Add CI workflow**

The workflow triggers on pull requests and pushes to `main`, grants `contents: read`, and contains these jobs:

```text
test: setup-go 1.25.x; go test ./...; go vet ./...
shell: bash scripts/run-action_test.sh; shellcheck scripts/*.sh
cross-build: matrix amd64,arm64; CGO_ENABLED=0 go build ./cmd/lazycat-action
fixture: build local binary; run action against copied testdata/static-app; reopen produced LPK
```

Pin the actions exactly as follows:

```text
actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5
actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff
actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02
```

The fixture job sets `LAZYCAT_ACTION_BINARY` and verifies outputs contain `target-platform=linux/amd64`.

- [ ] **Step 2: Write bilingual quick starts**

Both READMEs cross-link at the top and include:

```yaml
name: Build LPK
on:
  push:
    tags: ['v*']

permissions:
  contents: read

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: ca-x/lazycat-github-action@v1
        id: lazycat
        with:
          operation: build
          version: ${{ github.ref_name }}
      - uses: actions/upload-artifact@v4
        with:
          name: lpk-${{ steps.lazycat.outputs.version }}
          path: ${{ steps.lazycat.outputs.lpk-path }}
```

They define LPK, `package.yml`, `lzc-build.yml`, Manifest, buildscript, basic lint versus official lint, Artifact versus Release Asset, and the explicit host/target matrix. They state that ARM64 Runner is supported but all copied images and compiled application programs remain `linux/amd64`. They also state that Milestone 1 has no image checking, PR creation, Release publishing, or store submission yet.

- [ ] **Step 3: Update ignored generated files**

`.gitignore` must contain:

```text
/dist/
/.lazycat-action/
*.lpk
```

- [ ] **Step 4: Run the full local release gate**

Run:

```bash
gofmt -w cmd internal
go mod tidy
go test ./...
go vet ./...
bash scripts/run-action_test.sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/lazycat-action-amd64 ./cmd/lazycat-action
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /tmp/lazycat-action-arm64 ./cmd/lazycat-action
git diff --check
git status --short
```

Expected: tests and builds exit 0; only intended README, CI, and ignore changes remain before the commit.

- [ ] **Step 5: Commit the Milestone 1 release gate**

```bash
git add .github/workflows/ci.yml README.md README.zh-CN.md .gitignore
git commit -m "docs: add Milestone 1 usage and verification"
```

- [ ] **Step 6: Review and merge Milestone 1**

Run:

```bash
git log --oneline --decorate main..milestone/1-foundation
git diff --check main...milestone/1-foundation
go test ./...
go vet ./...
bash scripts/run-action_test.sh
```

Expected: seven focused implementation commits after the plan commit, no whitespace errors, and every verification command exits 0. Then fast-forward or merge `milestone/1-foundation` into `main`, push `main`, and confirm `origin/main` points to the merge result.

---

## Milestone 1 Completion Record

- `main` was fast-forwarded to `cf81446` on 2026-07-10.
- `go test -race ./...` passed for every package.
- `go vet ./...` passed.
- `scripts/run-action_test.sh` and ShellCheck 0.11.0 passed.
- `CGO_ENABLED=0` cross-builds produced Linux x86-64 and Linux ARM64 Action executables; both retain the fixed LazyCat target contract `linux/amd64`.
- The static fixture completed an end-to-end build through `scripts/run-action.sh`, produced an LPK, reopened it in tests, and matched the emitted SHA256.
- GoReleaser 2.17.0 `check` passed using the checksum-verified official Linux x86-64 release binary.
- Intentionally deferred to Milestone 2: OCI channel discovery, image target updates, LazyCat/direct/mirror delivery, PR/reusable-workflow orchestration, tag and GitHub Release Asset automation.
- Intentionally deferred to Milestone 3: official developer-platform authentication/publishing, the MiaoMiao private store, complete multi-language examples, Agent Skill/evals, and the public v1 release.
