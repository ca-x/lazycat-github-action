# Mutable Image Automatic Version Bump Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `update.version_source.bump: patch` so a mutable image tag increments the current stable package patch version only when its target-platform digest changes.

**Architecture:** Configuration validation identifies one mutable version-source image. Image selection chooses the newest `created` candidate without mapping the mutable tag to SemVer; delivery compares the selected digest with the currently delivered digest, skips equal images, and returns an auditable decision. The image flow bumps the package patch only after a changed digest and successful delivery proof.

**Tech Stack:** Go 1.24, `github.com/google/go-containerregistry`, `Masterminds/semver`, YAML configuration, GitHub composite/reusable Actions.

## Global Constraints

- `bump` accepts only `patch` and is valid only for an image version source using `channel: custom`, `sort: created`, and no `version_regex`.
- `allow_downgrade` must remain `false` in bump mode.
- Current package versions must be strict stable SemVer without prerelease or build metadata.
- Official publication continues to require `delivery.mode: lazycat`.
- Dry-run must inspect digests but must not copy images or write files.
- Existing configurations without `bump` must retain byte-compatible behavior.

---

### Task 1: Configuration contract

**Files:**
- Modify: `internal/config/types.go`
- Modify: `internal/config/load.go`
- Test: `internal/config/load_test.go`

**Interfaces:**
- Produces: `VersionSource.Bump string` normalized to `""` or `"patch"`.

- [ ] Add failing decode and validation tests for accepted `patch`, unknown values, Git sources, downgrade mode, non-created rules, and version regexes.
- [ ] Run `go test ./internal/config` and verify the new cases fail.
- [ ] Add `Bump string \`yaml:"bump"\`` and focused validation against the configured version-source image.
- [ ] Run `go test ./internal/config` and verify all cases pass.

### Task 2: Mutable candidate and patch versioning

**Files:**
- Modify: `internal/versioning/select.go`
- Test: `internal/versioning/select_test.go`

**Interfaces:**
- Produces: `SelectMutable(Rule, []Candidate) (Selection, error)` and `BumpPatch(string) (string, error)`.

- [ ] Add failing tests for newest-created selection, stable patch increment, and rejection of prerelease/build/overflow versions.
- [ ] Run `go test ./internal/versioning` and verify failure.
- [ ] Implement deterministic created-time selection and checked stable patch arithmetic.
- [ ] Run `go test ./internal/versioning` and verify success.

### Task 3: Digest-aware delivery

**Files:**
- Modify: `internal/delivery/delivery.go`
- Test: `internal/delivery/delivery_test.go`

**Interfaces:**
- Extends `delivery.Request` with `CurrentRef`, `CurrentDigest`, and `Mutable`.
- Extends `delivery.Result` with `CurrentDigest` and `DigestChanged`.

- [ ] Add failing direct, LazyCat, mirror, and dry-run tests for equal and changed digests.
- [ ] Run `go test ./internal/delivery` and verify failure.
- [ ] Pin direct/mirror mutable references by digest, persist the source digest in the Manifest upstream comment, and use authenticated LazyCat copy results for one-time legacy baseline migration without anonymously inspecting the private Registry.
- [ ] Run `go test ./internal/delivery` and verify success.

### Task 4: Image-flow bump decision and outputs

**Files:**
- Modify: `internal/imageflow/flow.go`
- Test: `internal/imageflow/flow_test.go`
- Modify: `internal/action/action_test.go`

**Interfaces:**
- Extends `ImageResult` with `currentDigest`, `digestChanged`, `bump`, `previousVersion`, and `selectedVersion`.

- [ ] Add failing tests for changed/equal digest, dry-run parity, delivery failure rollback, and one version-source bump in a multi-image flow.
- [ ] Run `go test ./internal/imageflow ./internal/action` and verify failure.
- [ ] Select mutable candidates, call digest-aware delivery, bump only after a changed digest, and preserve ordinary SemVer/downgrade behavior.
- [ ] Run `go test ./internal/imageflow ./internal/action` and verify success.

### Task 5: Public documentation and metadata

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `skills/lazycat-github-action/SKILL.md`
- Modify: `skills/lazycat-github-action/references/configuration.md`
- Modify: `skills/lazycat-github-action/assets/lazycat-action.yml`
- Modify: `action.yml`
- Test: `internal/metadata/action_test.go`

**Interfaces:**
- Documents the exact `bump: patch` YAML contract and failure behavior.

- [ ] Add the configuration example, delivery semantics, output fields, and migration prerequisites in English, Chinese, and Skill references.
- [ ] Set the composite bootstrap version to `v1.1.19` and update metadata assertions.
- [ ] Run `go test ./internal/metadata` and verify success.

### Task 6: Verification and release

**Files:**
- Modify: `CHANGELOG.md` if present.

- [ ] Run `gofmt` on changed Go files.
- [ ] Run `go test -race ./...`, `go vet ./...`, `go build ./...`, `go run ./cmd/eval-skill`, and `actionlint`.
- [ ] Commit, push `main`, verify CI, create annotated `v1.1.19`, publish the GitHub Release, and move annotated `v1` only after the immutable release passes.
- [ ] Configure CoolMonitor and WeRSS with `bump: patch`, trigger both workflows, and verify versioned Release assets plus independent official/private results.
