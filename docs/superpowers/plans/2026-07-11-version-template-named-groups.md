# Version Template Named Groups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expand every named `version_regex` capture in `version_template` while preserving existing `{version}` behavior.

**Architecture:** Keep parsing and expansion inside `internal/versioning.mapVersion`. Build a name-to-capture map from the existing regexp match, replace exact `{name}` placeholders, reject unresolved placeholders through explicit validation, then retain the existing final SemVer validation.

**Tech Stack:** Go 1.25/1.26, `regexp`, table-driven Go tests, Markdown documentation, repository Skill evals.

## Global Constraints

- `version_regex` still requires a named `version` group.
- Existing `{version}` templates remain compatible.
- Unknown placeholders and invalid final SemVer fail closed.
- No new runtime dependency.
- GitHub Action consumers continue using `@v1`.

---

### Task 1: Named capture expansion

**Files:**
- Modify: `internal/versioning/select.go`
- Test: `internal/versioning/select_test.go`

**Interfaces:**
- Consumes: `mapVersion(rule Rule, tag string) (string, error)`
- Produces: the same function signature with named placeholder expansion.

- [ ] Add a table case mapping regex groups `version=20260603`, `build=1` with template `{version}.{build}.0` to `20260603.1.0`.
- [ ] Add a table case where `{missing}` remains unresolved and assert an error mentioning `unresolved version template placeholder`.
- [ ] Run `go test ./internal/versioning` and confirm the new cases fail.
- [ ] Expand all named groups in `mapVersion`, scan for remaining `{[A-Za-z][A-Za-z0-9_]*}` placeholders, and return an error before SemVer validation when one remains.
- [ ] Run `go test ./internal/versioning` and confirm all cases pass.

### Task 2: Documentation and Skill contract

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `skills/lazycat-github-action/SKILL.md`
- Modify: `skills/lazycat-github-action/references/configuration.md`
- Modify: `internal/metadata/skill_test.go`

**Interfaces:**
- Consumes: named placeholders defined by Task 1.
- Produces: user-facing syntax and a repository contract preventing documentation drift.

- [ ] Document `version_regex: '^(?P<version>\\d{8})\\.0*(?P<build>[1-9]\\d*)$'` with `version_template: '{version}.{build}.0'`.
- [ ] State that unknown placeholders and non-SemVer results fail closed.
- [ ] Add metadata assertions for `{build}` and named capture documentation.
- [ ] Run `go test ./internal/metadata` and confirm it passes.

### Task 3: Release verification

**Files:**
- Modify: `action.yml` bootstrap version only if a new Action release is required.

**Interfaces:**
- Consumes: Tasks 1-2.
- Produces: a released `v1.x.y` and floating `v1` pointing to the same commit.

- [ ] Run `gofmt`, `git diff --check`, JSON validation, `go test -race ./...`, `go vet ./...`, `go mod verify`, actionlint, bootstrap tests, ShellCheck, amd64/arm64 builds, and GoReleaser snapshot verification.
- [ ] Commit and push the exact verified HEAD.
- [ ] Wait for all GitHub CI jobs to pass.
- [ ] Tag the next patch release, verify archives/checksums/SBOM/attestations, and confirm floating `v1` points to the release commit.
