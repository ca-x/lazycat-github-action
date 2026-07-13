# Image Tag Updated Sort Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add opt-in Docker Hub update-time sorting and an optional project target architecture that defaults to amd64.

**Architecture:** Extend the configuration and versioning contracts with an `updated` sort. Add a bounded Docker Hub metadata adapter behind the registry client, rank tags before manifest inspection, and replace fixed target constants with an immutable `platform.Target` passed through inspection, delivery, build, validation, and publication.

**Tech Stack:** Go, `go-containerregistry`, Docker Hub HTTP API, YAML v3, GitHub Actions.

## Global Constraints

- Stable and beta default to `semver` exactly as before.
- `updated` means Docker Hub `last_updated`, never OCI `config.created`.
- Updated ordering is timestamp descending, mapped SemVer descending, tag descending.
- Updated sorting is fail-closed and Docker Hub-only until another registry exposes an equivalent timestamp.
- `update.allow_downgrade: false` remains effective.
- Registry response bodies, credentials, and tokens must not enter errors or logs.
- `project.target_arch` defaults to `amd64`, accepts `amd64` or `arm64`, and always uses Linux.
- Target architecture is passed explicitly and must not use mutable package-level state.

---

### Task 1: Configuration and ranking contract

**Files:**
- Modify: `internal/config/load.go`
- Modify: `internal/config/load_test.go`
- Modify: `internal/versioning/select.go`
- Modify: `internal/versioning/select_test.go`

**Interfaces:**
- Produces: `versioning.SortUpdated = "updated"`
- Produces: `versioning.Candidate.Updated time.Time`
- Produces: `versioning.RankUpdated(Rule, []Candidate) ([]Selection, error)`

- [ ] Add a config test where stable plus `sort: updated` loads successfully and nightly plus `sort: updated` fails.
- [ ] Run `go test ./internal/config -run 'TestLoad' -count=1` and require RED.
- [ ] Accept `updated` only for stable, beta, and custom rules while preserving all defaults.
- [ ] Run the focused config test and require GREEN.
- [ ] Add a versioning test where recently updated `v1.2.15` ranks before older `v1.2.26`, plus equal-time SemVer and tag tie-break cases.
- [ ] Run `go test ./internal/versioning -run 'Updated' -count=1` and require RED.
- [ ] Add `Updated`, `SortUpdated`, validation, selection, and tag-only ranking with zero-time rejection.
- [ ] Run `go test ./internal/versioning -count=1` and require GREEN.

### Task 2: Docker Hub metadata and registry selection

**Files:**
- Modify: `internal/registry/client.go`
- Modify: `internal/registry/client_test.go`
- Create: `internal/registry/dockerhub.go`
- Create: `internal/registry/dockerhub_test.go`

**Interfaces:**
- Consumes: `versioning.RankUpdated`
- Produces: `TagFilter.UpdatedRule *versioning.Rule`
- Produces: internal `tagMetadata.Updates(context.Context, name.Repository, map[string]struct{}) (map[string]time.Time, error)`

- [ ] Add an HTTP fixture test that returns two Docker Hub metadata pages and proves `last_updated` is preserved for requested tags.
- [ ] Run `go test ./internal/registry -run 'DockerHub' -count=1` and require RED.
- [ ] Implement a timeout-bound, response-bounded, paginated Docker Hub metadata client with escaped namespace/repository paths and safe status errors.
- [ ] Add failure tests for non-Docker Hub repositories, missing timestamps, non-2xx status, oversized responses, cancellation, and more than 10,000 tags.
- [ ] Run the Docker Hub tests and require GREEN.
- [ ] Add a registry candidate test where update ranking skips a newer arm64-only tag and returns the next `linux/amd64` tag with both `Updated` and image metadata.
- [ ] Run the focused registry test and require RED.
- [ ] Wire `UpdatedRule` into candidate discovery and inspect ranked tags only until the first usable target image is found.
- [ ] Run `go test ./internal/registry -count=1` and require GREEN.

### Task 3: Image flow and safety behavior

**Files:**
- Modify: `internal/imageflow/flow.go`
- Modify: `internal/imageflow/flow_test.go`

**Interfaces:**
- Consumes: `versioning.SortUpdated` and `registry.TagFilter.UpdatedRule`
- Preserves: `imageflow.ErrVersionDowngrade`

- [ ] Add a flow test proving `sort: updated` populates `UpdatedRule` and selects the registry result.
- [ ] Run the focused test and require RED.
- [ ] Wire updated rules into registry filtering and treat updated LazyCat delivery as eligible for mutable-tag refresh.
- [ ] Add a test proving a lower updated selection remains blocked before delivery when `allow_downgrade` is false.
- [ ] Run `go test ./internal/imageflow -count=1` and require GREEN.

### Task 4: Configurable target architecture

**Files:**
- Modify: `internal/platform/platform.go`
- Modify: `internal/platform/platform_test.go`
- Modify: `internal/config/types.go`
- Modify: `internal/config/load.go`
- Modify: `internal/config/load_test.go`
- Modify: `internal/registry/client.go`
- Modify: `internal/registry/client_test.go`
- Modify: `internal/delivery/delivery.go`
- Modify: `internal/delivery/delivery_test.go`
- Modify: `internal/build/build.go`
- Modify: `internal/build/build_test.go`
- Modify: `internal/lpkcheck/check.go`
- Modify: `internal/lpkcheck/check_test.go`
- Modify: `internal/imageflow/flow.go`
- Modify: `internal/imageflow/flow_test.go`
- Modify: `internal/publishflow/flow.go`
- Modify: `internal/publishflow/flow_test.go`
- Modify: `internal/action/action.go`
- Modify: `internal/action/action_test.go`

**Interfaces:**
- Produces: `config.Project.TargetArch string` from `project.target_arch`
- Produces: immutable `platform.Target{OS, Arch}` with `Platform() string`
- Consumes: one target value across registry, delivery, build, LPK validation, image flow, publish flow, and Action output

- [ ] Add config/platform tests proving omission yields amd64, explicit arm64 is accepted, and other values fail.
- [ ] Run `go test ./internal/config ./internal/platform -count=1` and require RED.
- [ ] Implement target normalization without mutable global state; run focused tests and require GREEN.
- [ ] Add registry and delivery tests proving arm64 manifest selection, mirror verification, and LazyCat copy `Platform: "arm64"`.
- [ ] Run `go test ./internal/registry ./internal/delivery -count=1` and require RED.
- [ ] Pass the target through registry and delivery interfaces; run focused tests and require GREEN.
- [ ] Add build/LPK tests proving `LAZYCAT_TARGET_ARCH=arm64`, `LAZYCAT_TARGET_PLATFORM=linux/arm64`, and arm64 result metadata.
- [ ] Run `go test ./internal/build ./internal/lpkcheck -count=1` and require RED, implement explicit target parameters, then require GREEN.
- [ ] Add image-flow, publish-flow, and Action tests proving the configured target reaches selection, validation, logs, and outputs while omitted configuration remains amd64.
- [ ] Pass the target explicitly through those flows and run their focused test suites until GREEN.

### Task 5: Documentation and Skill contract

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `skills/lazycat-github-action/SKILL.md`
- Modify: `skills/lazycat-github-action/references/configuration.md`
- Modify: `skills/lazycat-github-action/evals/evals.json`
- Modify: `skills/lazycat-github-action/test-prompts.json`
- Modify: `internal/metadata/skill_test.go`

**Interfaces:**
- Documents: `sort: updated`, Docker Hub-only scope, tie-breaks, downgrade interaction, and Sublink-style use.

- [ ] Extend metadata tests and eval contracts first; run `go test ./internal/metadata -count=1` and require RED.
- [ ] Document updated/created/semver distinctions in both READMEs, the Skill, and configuration reference.
- [ ] Add an eval and Chinese test prompt for an older SemVer tag that was updated more recently.
- [ ] Document `project.target_arch`, amd64 default, arm64 support, Runner/target independence, and project-wide target consistency.
- [ ] Run `go test ./internal/metadata -count=1` and JSON parsing tests and require GREEN.

### Task 6: Verification, consumer experiment, and release

**Files:**
- Modify in consumer repository: `.github/lazycat-action.yml`

**Interfaces:**
- Produces: next patch tag and floating `v1`
- Verifies: `lazycat-contrib/sublink-pro-lzcapp`

- [ ] Run `gofmt` and `git diff --check`.
- [ ] Run `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...`.
- [ ] Run `bash scripts/run-action_test.sh` and actionlint over repository and example workflows.
- [ ] Commit and push the implementation.
- [ ] Wait for repository CI and require success.
- [ ] Publish the next patch release, move floating `v1`, and verify checksums, SBOMs, attestations, and Marketplace resolution.
- [ ] Add `sort: updated` to `lazycat-contrib/sublink-pro-lzcapp`, trigger the reusable workflow, and verify logs show updated sorting and the expected tag selection.
- [ ] Confirm the consumer workflow succeeds without an unintended SemVer upgrade to `v1.2.26`.
