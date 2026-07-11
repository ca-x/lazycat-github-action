# Image Version Downgrade Guard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent image automation from lowering the LazyCat application SemVer unless the repository explicitly opts in.

**Architecture:** Extend `config.Update` with a default-false boolean. `imageflow.Flow.Check` compares only the selected version-source image version against the inspected project version before delivery, then returns a typed sentinel that `action` maps to a stable public error code.

**Tech Stack:** Go 1.25/1.26, Masterminds SemVer, google/go-containerregistry, YAML v3, GitHub Actions.

## Global Constraints

- `update.allow_downgrade` defaults to `false`.
- Lower versions are blocked before delivery and file writes.
- Equal versions remain eligible for digest/image-reference refresh.
- Non-version-source images are unaffected.
- No Secret or remote response body may enter diagnostics.

---

### Task 1: Configuration contract

**Files:**
- Modify: `internal/config/types.go`
- Modify: `internal/config/load_test.go`

**Interfaces:**
- Produces: `config.Update.AllowDowngrade bool`

- [ ] Add a load test proving omitted configuration is false and explicit `allow_downgrade: true` is true.
- [ ] Run `go test ./internal/config -count=1` and observe the new assertion fail before implementation.
- [ ] Add `AllowDowngrade bool \`yaml:"allow_downgrade"\`` to `config.Update`.
- [ ] Run `go test ./internal/config -count=1` and require PASS.

### Task 2: Pre-delivery downgrade guard

**Files:**
- Modify: `internal/imageflow/flow.go`
- Modify: `internal/imageflow/flow_test.go`
- Modify: `internal/action/action.go`
- Modify: `internal/action/action_test.go`

**Interfaces:**
- Produces: `imageflow.ErrVersionDowngrade`
- Produces: `action.CodeVersionDowngradeBlocked = "VERSION_DOWNGRADE_BLOCKED"`

- [ ] Add an Odoo-style test where created-time sorting selects `18.0.0` while the project is `19.0.0`; assert `errors.Is(err, imageflow.ErrVersionDowngrade)`, zero delivery calls, and zero Manifest writes.
- [ ] Run the focused test and require RED.
- [ ] Compare parsed selected/current SemVer immediately after version-source selection and return the sentinel when selected is lower and opt-in is false.
- [ ] Run the focused test and require GREEN.
- [ ] Add tests proving explicit opt-in permits downgrade and an equal version still refreshes a changed image.
- [ ] Map the sentinel to `VERSION_DOWNGRADE_BLOCKED` and verify through the public Action interface.
- [ ] Run `go test ./internal/imageflow ./internal/action -count=1` and require PASS.

### Task 3: Documentation, Skill, and release gates

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `skills/lazycat-github-action/SKILL.md`
- Modify: `skills/lazycat-github-action/references/configuration.md`
- Modify: `skills/lazycat-github-action/test-prompts.json`
- Modify: `internal/metadata/skill_test.go`

**Interfaces:**
- Documents: default protection, explicit rollback opt-in, equal-version refresh behavior.

- [ ] Add English/Chinese configuration examples and a Skill failure row for blocked downgrade.
- [ ] Add a Skill prompt requiring the default guard and explicit user confirmation before enabling downgrade.
- [ ] Run metadata and JSON validation tests.
- [ ] Run `go test -race ./...`, `go vet ./...`, actionlint, ShellCheck, bootstrap smoke tests, dual-architecture builds, and GoReleaser snapshot verification.
- [ ] Commit the program/docs changes, publish the next patch tag, move floating `v1`, and verify CI and Release artifacts.
- [ ] Re-run affected application workflows and verify versioned LPK, SHA256, Release, and store outcomes.
