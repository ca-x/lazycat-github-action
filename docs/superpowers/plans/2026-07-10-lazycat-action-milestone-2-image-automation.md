# LazyCat Action Milestone 2 Image Automation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `executing-plans` task-by-task. The repository owner requires inline main-agent execution; do not dispatch subagents. Track every step with its checkbox.

**Goal:** Add deterministic OCI image update checks, explicit service/application image edits, amd64 LazyCat/direct/mirror delivery, structured copy results, and reusable GitHub workflows for PR, Artifact, tag, and Release Asset automation.

**Architecture:** Pure channel/version selection lives in `internal/versioning`; OCI network access lives in `internal/registry`; Manifest edits and delivery are independent adapters composed by `internal/imageflow`. The existing Action gains a real `check` operation while `build` remains usable for Git/tag and merged-PR builds. A pinned reusable workflow handles GitHub mutations without embedding GitHub API behavior in the core binary.

**Tech Stack:** Go 1.25, `github.com/google/go-containerregistry v0.21.7`, `github.com/Masterminds/semver/v3 v3.5.0`, `github.com/lib-x/lzc-toolkit-go v0.1.0`, GitHub reusable workflows, existing composite Action bootstrap.

## Global Constraints

- Action hosts remain Linux amd64/arm64; image inspection and delivery always select `linux/amd64`.
- LazyCat copying must call `appstore.CopyImage` with `Platform: "amd64"`; it is remote registry-to-registry and does not call local Docker.
- `update.strategy` defaults to `pull`.
- Never infer a main service. Every image target is `service + service name` or `application`; one explicit image ID drives the package version.
- `stable` selects highest release SemVer, `beta` selects highest prerelease SemVer, `nightly` sorts regex-filtered tags by amd64 image creation time, and `custom` requires explicit sorting.
- Source Registry inspection always uses the configured source; mirror/direct only change the runtime reference.
- Official publishing configuration rejects every `direct` or `mirror` image before network access.
- Manifest updates retain or add `# upstream: <source-ref>` and preserve unrelated comments/fields.
- `image-results` returns inspection, selected digest, delivery mode, delivered reference, copied state, and LazyCat copy result; logs alone are insufficient.
- Third-party Actions in repository workflow files use complete commit SHAs.
- Use TDD and one focused commit per task.

---

## File Structure

```text
internal/config/types.go               extend image/channel/delivery model
internal/config/load.go                normalize and validate image configuration
internal/config/load_test.go
internal/versioning/select.go          pure tag filtering, SemVer and nightly version mapping
internal/versioning/select_test.go
internal/registry/client.go            OCI tags, indexes, amd64 digest and creation time
internal/registry/client_test.go
internal/manifestedit/images.go         exact YAML target read/update and upstream comments
internal/manifestedit/images_test.go
internal/delivery/delivery.go          lazycat/direct/mirror resolution
internal/delivery/delivery_test.go
internal/imageflow/flow.go             validate, inspect, deliver, atomically edit
internal/imageflow/flow_test.go
internal/action/action.go               check operation and image outputs
internal/action/action_test.go
internal/githubio/env.go                strategy/channel/image outputs
internal/githubio/env_test.go
cmd/lazycat-action/main.go              default registry/delivery dependencies
action.yml                              additive update-strategy/channel outputs
.github/workflows/lazycat.yml           reusable PR/Artifact/Release workflow
internal/metadata/action_test.go        workflow/action contract tests
.github/workflows/ci.yml                registry and workflow integration gates
testdata/image-app/                     two-service fixture with explicit web target
README.md                               Docker/channel/delivery/reusable-workflow guide
README.zh-CN.md                         Chinese equivalent and cross-link
```

---

### Task 1: Image configuration and pure channel selection

**Interfaces:**

- Produces: `versioning.Select(rule Rule, candidates []Candidate) (Selection, error)`.
- Produces: fully normalized `config.Image` with channel/sort/delivery defaults.
- Consumes: existing `config.Load` and SemVer validation rules.

- [ ] **Step 1: Create `milestone/2-image-automation` from current `main` in an ignored worktree**

```bash
git worktree add .worktrees/milestone-2-image-automation -b milestone/2-image-automation
```

- [ ] **Step 2: Write configuration tests**

Cover exact success cases:

```yaml
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
    channel: stable
    delivery:
      mode: lazycat
```

```yaml
images:
  - id: runtime
    target: application
    source: ghcr.io/acme/runtime
    channel: custom
    sort: created
    tag_regex: '^edge-'
    version_regex: '^edge-(?P<version>\d+\.\d+\.\d+)$'
    delivery:
      mode: mirror
      image_template: ghcr.1ms.run/acme/runtime:{tag}
      require_digest_match: true
```

Assert defaults `channel=stable`, `sort=semver`, `version_template={version}`, and `delivery.mode=lazycat`. Assert these failures: missing/duplicate ID, missing source, service target without service, application target with service, unknown target/channel/sort/mode, nightly/custom without `tag_regex`, custom without `sort`, invalid regex, mirror without template, `version_source.image` not found, and official store combined with direct/mirror.

- [ ] **Step 3: Write version selection tests**

Use:

```go
candidates := []versioning.Candidate{
	{Tag: "v1.9.0", Digest: "sha256:190", Created: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
	{Tag: "v2.0.0-beta.1", Digest: "sha256:beta1", Created: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)},
	{Tag: "v2.0.0-rc.1", Digest: "sha256:rc1", Created: time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)},
	{Tag: "v2.0.0", Digest: "sha256:200", Created: time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)},
}
```

Expected: stable=`v2.0.0`, beta=`v2.0.0-rc.1`. Nightly selects the newest `Created` after regex/exclusion and maps digest `sha256:a1b2c3d4e5f6...` at `2026-07-10T15:30:20Z` to `0.0.0-nightly.20260710153020.a1b2c3d4e5f6`. Custom `sort=semver` uses extracted versions; custom `sort=created` uses time. Empty candidate sets, invalid named capture, invalid mapped SemVer, and digest shorter than 12 hex characters fail.

- [ ] **Step 4: Implement types and selection**

Define:

```go
type Channel string
const (
	ChannelStable  Channel = "stable"
	ChannelBeta    Channel = "beta"
	ChannelNightly Channel = "nightly"
	ChannelCustom  Channel = "custom"
)

type Sort string
const (
	SortSemVer  Sort = "semver"
	SortCreated Sort = "created"
)

type Rule struct {
	Channel         Channel
	Sort            Sort
	TagRegex        *regexp.Regexp
	ExcludeRegex    *regexp.Regexp
	VersionRegex    *regexp.Regexp
	VersionTemplate string
}

type Candidate struct {
	Tag     string
	Digest string
	Created time.Time
}

type Selection struct {
	Candidate Candidate
	Version   string
}
```

Use `Masterminds/semver/v3` for precedence. Stable excludes prereleases; beta requires prerelease. Normalize the `night` config alias to `nightly`. Do not silently fall back from an invalid tag/version.

- [ ] **Step 5: Verify and commit**

```bash
go test ./internal/config ./internal/versioning -v
go vet ./internal/config ./internal/versioning
git add go.mod go.sum internal/config internal/versioning
git commit -m "feat: select image versions by channel"
```

---

### Task 2: OCI registry client fixed to linux/amd64

**Interfaces:**

- Consumes: `versioning.Rule` and `authn.DefaultKeychain`.
- Produces: `(*registry.Client).Candidates(ctx, source string) ([]versioning.Candidate, error)`.
- Produces: `(*registry.Client).Inspect(ctx, reference string) (registry.Image, error)`.

- [ ] **Step 1: Write an in-memory Registry integration test**

Start `httptest.NewServer(registry.New())`, push tagged images/indexes through `remote.Write`/`remote.WriteIndex`, and include both:

```text
linux/amd64 created 2026-07-10T15:30:20Z
linux/arm64 created 2026-07-11T15:30:20Z
```

Assert the client returns the amd64 manifest digest/time even though arm64 is newer. Add tags spanning stable, beta, and nightly. Verify repository tag pagination by limiting the test Registry page size or by an HTTP fixture returning multiple `Link` pages. Test anonymous Bearer challenge, malformed manifest, index without amd64, digest mismatch, cancellation, and response size limits.

- [ ] **Step 2: Implement Registry contracts**

```go
type Image struct {
	Reference string
	Digest    string
	Created   time.Time
	Platform  string
}

type Client struct {
	Options []remote.Option
}

func New(options ...remote.Option) *Client
func (client *Client) Candidates(ctx context.Context, source string) ([]versioning.Candidate, error)
func (client *Client) Inspect(ctx context.Context, reference string) (Image, error)
```

Parse `source` as a repository with `name.WeakValidation`. `Candidates` lists all tags then inspects each reference with the same context/options. `Inspect` uses a descriptor/index manifest to select only `v1.Platform{OS:"linux", Architecture:"amd64"}`; single manifests are accepted only after reading the image config. Return the platform-specific digest, not a multi-platform index digest. Default options are `remote.WithContext(ctx)` and `remote.WithAuthFromKeychain(authn.DefaultKeychain)`.

- [ ] **Step 3: Verify and commit**

```bash
go test ./internal/registry -v
go test -race ./internal/registry
go vet ./internal/registry
git add internal/registry
git commit -m "feat: inspect amd64 OCI image candidates"
```

---

### Task 3: Exact Manifest image editing

**Interfaces:**

- Produces: `manifestedit.Read(filename string, targets []Target) ([]Current, error)`.
- Produces: `manifestedit.Apply(filename string, updates []Update) ([]Change, error)`.
- Does not infer any target or service.

- [ ] **Step 1: Write YAML preservation tests**

Fixture:

```yaml
application:
  subdomain: app
services:
  db:
    image: postgres:17
  web:
    # retain this comment
    # upstream: ghcr.io/acme/web:v1.0.0
    image: registry.lazycat.cloud/acme/web:v1.0.0 # runtime
```

Update only `service:web` and assert `db` is byte-semantically unchanged, both unrelated comments remain, upstream becomes the selected source, and inline runtime comment remains. Test `target=application`, missing service, missing application, duplicate target, service without image (insert), and atomic rollback when any target is invalid.

- [ ] **Step 2: Implement exact contracts**

```go
type TargetKind string
const (
	TargetService     TargetKind = "service"
	TargetApplication TargetKind = "application"
)

type Target struct { ID string; Kind TargetKind; Service string }
type Current struct { ID, RuntimeRef, UpstreamRef string }
type Update struct { Target Target; SourceRef, RuntimeRef string }
type Change struct { ID string; Changed bool; OldRuntimeRef, NewRuntimeRef, OldUpstreamRef, NewUpstreamRef string }
```

Parse one `yaml.Node`, validate every target before mutation, update/insert only the image scalar, replace only comment lines beginning `upstream:`, and atomically write once with original mode and directory fsync. Reject symbolic-link Manifest paths.

- [ ] **Step 3: Verify and commit**

```bash
go test ./internal/manifestedit -v
go vet ./internal/manifestedit
git add internal/manifestedit
git commit -m "feat: update explicit manifest image targets"
```

---

### Task 4: LazyCat, direct, and mirror delivery

**Interfaces:**

- Consumes: `registry.Client.Inspect`, `appstore.Client.CopyImage`.
- Produces: `(*delivery.Resolver).Deliver(ctx, request Request) (Result, error)`.

- [ ] **Step 1: Write adapter tests with fake copier/registry**

Assertions:

```text
lazycat -> CopyImageRequest.Image is exact source ref
lazycat -> CopyImageRequest.Platform == "amd64"
lazycat -> Result.RuntimeRef equals CopyImageResult.LazyCatImage
direct  -> no copier/registry call; RuntimeRef equals source ref
mirror  -> template receives {tag}; digest comparison uses linux/amd64
mirror mismatch with require_digest_match -> error before Manifest mutation
```

Serialize final copy data including source image, platform, LazyCat image, finished state, and final layer progress.

- [ ] **Step 2: Implement delivery contracts**

```go
type Copier interface {
	CopyImage(context.Context, appstore.CopyImageRequest) (appstore.CopyImageResult, error)
}

type ImageInspector interface {
	Inspect(context.Context, string) (registry.Image, error)
}

type Request struct {
	Image config.Image
	Tag string
	SourceRef string
	SourceDigest string
	DryRun bool
}

type Result struct {
	Mode string
	RuntimeRef string
	Copied bool
	CopyResult *appstore.CopyImageResult
}
```

For mirror templates support `{tag}`, `{digest}`, and `{source}`. Dry-run never calls copier or mirror inspection. LazyCat token resolution is `LAZYCAT_TOKEN`, then `LZC_CLI_TOKEN`; missing token produces a typed authentication error. Account/password login remains Milestone 3.

- [ ] **Step 3: Verify and commit**

```bash
go test ./internal/delivery -v
go vet ./internal/delivery
git add internal/delivery
git commit -m "feat: deliver images through LazyCat direct or mirror modes"
```

---

### Task 5: Image flow and Action check operation

**Interfaces:**

- Consumes: config, versioning, registry, manifestedit, delivery.
- Produces: `(*imageflow.Flow).Check(ctx, Request) (Result, error)`.
- Extends Action outputs with `update-strategy` and `channel`; preserves existing outputs.

- [ ] **Step 1: Write image-flow tests**

Use two configured services `db` and `web`, with `web` as `version_source.image`. Assert:

- no `image-id` checks all configured images;
- `image-id=web` checks only web;
- nonexistent image ID fails;
- no target is inferred from order;
- all targets validate before any copy;
- unchanged upstream skips delivery and edit;
- changed web selects version, delivers, edits web only, and returns `Changed=true`;
- dry-run returns the planned source/digest and never copies/edits;
- each result fixes `platform=linux/amd64` and includes structured copy result;
- multiple updates are written atomically.

- [ ] **Step 2: Define high-level results**

```go
type ImageResult struct {
	ID string `json:"id"`
	Target string `json:"target"`
	Service string `json:"service,omitempty"`
	Platform string `json:"platform"`
	SourceRef string `json:"sourceRef"`
	SourceDigest string `json:"sourceDigest"`
	DeliveryMode string `json:"deliveryMode"`
	DeliveredRef string `json:"deliveredRef"`
	Copied bool `json:"copied"`
	CopyResult *CopyResult `json:"copyResult,omitempty"`
}

type Result struct {
	Changed bool
	Version string
	Channel string
	Images []ImageResult
}
```

- [ ] **Step 3: Implement `imageflow.Check`**

Resolve selected config images, read all current Manifest targets first, call Registry candidates and `versioning.Select`, build `sourceRef=<source>:<tag>`, compare it to the upstream comment, deliver only changed sources, then call `manifestedit.Apply` once. The configured version-source image sets the package version only when included; otherwise keep the current package version.

- [ ] **Step 4: Extend Action orchestration**

Add dependency:

```go
CheckImages func(context.Context, imageflow.Request) (imageflow.Result, error)
```

`operation=check` loads/inspects, runs image flow, updates `package.yml.version`, re-inspects, and builds a validation LPK only when a file changed. `operation=build` uses the explicit input version or current `package.yml.version`; it does not query Registry or copy images. Encode `image-results` from the typed slice. Map failures to `VERSION_NOT_FOUND`, `PLATFORM_NOT_FOUND`, `IMAGE_COPY_FAILED`, or `BUILD_FAILED`.

- [ ] **Step 5: Verify and commit**

```bash
go test ./internal/imageflow ./internal/action ./internal/githubio ./cmd/lazycat-action -v
go test -race ./internal/imageflow ./internal/action
go vet ./...
git add internal/imageflow internal/action internal/githubio cmd action.yml
git commit -m "feat: check and update configured application images"
```

---

### Task 6: Reusable PR, Artifact, tag, and Release workflow

**Interfaces:**

- Produces: `.github/workflows/lazycat.yml` callable through `workflow_call`.
- Consumes: Action outputs `changed`, `update-strategy`, `version`, `tag`, `lpk-path`, `sha256`, and `image-results`.

- [ ] **Step 1: Add workflow metadata tests**

Parse the workflow and assert explicit inputs `config`, `operation`, `image-id`, `dry-run`, `toolchains`, `go-version`, `node-version`, `rust-toolchain`, `node-package-manager`, `enable-qemu`; secrets for LazyCat/source Registry; outputs matching the Action; and permissions `contents:write`, `pull-requests:write`.

- [ ] **Step 2: Implement pinned setup and authentication steps**

Use exact pins:

```text
actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5
actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff
actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020
dtolnay/rust-toolchain@4be7066ada62dd38de10e7b70166bc74ed198c30
docker/login-action@c94ce9fb468520275223c153574b00df6fe4bcc9
docker/setup-qemu-action@c7c53464625b32c7a7e944ae62b3e17d2b600130
docker/setup-buildx-action@8d2750c68a42422c14e847fe6c8ac0403b4cbd6f
actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02
peter-evans/create-pull-request@22a9089034f40e5a961c8808d113e2c98fb63676
actions/github-script@f28e40c7f34bde8b3046d885e986cb6290c5673b
softprops/action-gh-release@3bb12739c298aeb8a4eeaf626c5b8d85266b0e65
```

Toolchain inputs may contain combinations. Empty version inputs use `go.mod`, `.node-version`, or `rust-toolchain.toml`; missing both explicit and project version fails. Source Registry credentials are optional and populate Docker config for `authn.DefaultKeychain`.

- [ ] **Step 3: Implement update-strategy branches**

- `pull`: upload the built LPK as Workflow Artifact and create/update branch `lazycat/update-<image-id-or-all>` with `peter-evans/create-pull-request`; never create Release/store submission.
- `publish`: commit managed YAML with `[skip ci]`, push, create `v<version>` only when absent, and upload the LPK with `softprops/action-gh-release`.
- tag/release build: upload to the existing/created Release; do not move an existing external tag.
- existing same-name/same-SHA Asset is reused; different content fails.
- after successful tag/release upload, `actions/github-script` synchronizes `package.yml.version` to the default branch only when needed.

- [ ] **Step 4: Add caller examples and CI syntax validation**

Add schedule, manual, push-main, tag, and release examples. CI parses all workflow YAML, checks complete SHA pins, and runs `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7`.

- [ ] **Step 5: Verify and commit**

```bash
go test ./internal/metadata -v
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/*.yml
git diff --check
git add .github/workflows internal/metadata
git commit -m "feat: automate LazyCat pull requests and releases"
```

---

### Task 7: Docker documentation, fixtures, and Milestone 2 merge gate

- [ ] **Step 1: Add a two-service fixture**

`testdata/image-app` contains database service `db`, web service `web`, explicit source config for `web`, a fake Registry integration setup, and expected upstream/runtime comments. It proves the database image is never treated as the main service.

- [ ] **Step 2: Expand both READMEs with complete runnable examples**

Document stable+LazyCat, stable+mirror, beta, nightly, `image-id`, default PR, direct publish, no-copy/direct, Docker requirements, source Registry auth through Docker config, ARM64 Runner with amd64 copied image, image-results JSON, Artifact versus Release Asset, and merged-PR publish. State that direct/mirror cannot publish to the official store.

- [ ] **Step 3: Run the full Milestone 2 gate**

```bash
gofmt -w cmd internal
go mod tidy
go test -race ./...
go vet ./...
bash scripts/run-action_test.sh
shellcheck scripts/*.sh testdata/*/scripts/*.sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/lazycat-action-amd64 ./cmd/lazycat-action
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /tmp/lazycat-action-arm64 ./cmd/lazycat-action
goreleaser check
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/*.yml
git diff --check
```

- [ ] **Step 4: Commit documentation and fixture**

```bash
git add README.md README.zh-CN.md testdata .github/workflows/ci.yml
git commit -m "docs: add image automation and release examples"
```

- [ ] **Step 5: Review, merge, and push**

Review `main...milestone/2-image-automation`, fix all critical/important findings inline without subagents, rerun the full gate, fast-forward/merge into `main`, append the completion record below, commit it, and push `origin/main`.

---

## Milestone 2 Completion Protocol

Execution is complete only after this plan is amended with the final commit SHA, test/race/vet results, Registry fixture results, amd64/arm64 build results, workflow lint results, and the exact Milestone 3 items intentionally deferred.

## Milestone 2 Completion Record

- Completed on 2026-07-10.
- Reviewed implementation head: `5e27e32` (`fix: harden image automation workflows`).
- `go test -race ./...`: passed for all packages, including OCI Registry and two-service image-flow fixtures.
- `go vet ./...`: passed.
- `bash scripts/run-action_test.sh`: passed for checksum verification and amd64/arm64 bootstrap selection.
- ShellCheck `v0.10.0`: passed for `scripts/*.sh` and fixture build scripts.
- Linux cross-builds: `CGO_ENABLED=0` passed for `GOARCH=amd64` and `GOARCH=arm64`.
- GoReleaser `v2.17.0 check`: passed.
- actionlint `v1.7.7`: passed for every repository workflow.
- English, Chinese, and Chinese design-spec punctuation gates: passed.
- In-memory OCI Registry verification covers multi-platform selection, ARM-only tag skipping, filtered tag inspection, cancellation, and fixed `linux/amd64` digest/creation metadata.
- The `testdata/image-app` fixture proves that an explicit `web` target updates without treating the `db` service as the main service.

Milestone 3 intentionally retains these items:

- account/password exchange for a developer-platform token in CI;
- LazyCat official application creation, Testflight, LPK submission, changelog locales, and typed submission results;
- Miao private-store create-version and upload flows using `APPSTORE_URL`, `APPSTORE_TOKEN`, Release Asset URL, and SHA256, with optional connection metadata;
- store publication idempotency, retry policy, and release-to-store output contracts;
- documentation and tests for local lzc-cli token-file import in CI bootstrap;
- any source Registry credentials beyond what the actual lzc-cli 2.0.8 `CopyImage` protocol supports.
