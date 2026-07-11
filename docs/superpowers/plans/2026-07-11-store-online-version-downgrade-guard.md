# Store Online-Version Downgrade Guard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent an older verified LPK from being submitted to a store that already exposes a newer SemVer, while keeping each store publication independently successful.

**Architecture:** Extend the existing publish-flow reconciliation decision so it returns a typed skip reason after anonymous version lookup. Use the existing Masterminds SemVer dependency for ordering, propagate the reason through official/private result JSON, and preserve exact-equality-only behavior for non-SemVer values.

**Tech Stack:** Go 1.25, `github.com/Masterminds/semver/v3`, `log/slog`, Go tests, Markdown Skill metadata tests, GitHub Actions.

## Global Constraints

- `skip_if_version_exists` remains opt-in and lookup failures other than not-found remain fail-closed.
- `update.allow_downgrade: false` remains the safe default.
- Non-SemVer values must never be ordered by lexical comparison.
- Official and private store decisions remain independent.
- No credentials or upstream response bodies may be logged.
- Do not use sub-agents.

---

### Task 1: Publish-flow downgrade decision

**Files:**
- Modify: `internal/publishflow/flow.go`
- Modify: `internal/publishflow/flow_test.go`
- Modify: `internal/store/official/publish.go`
- Modify: `internal/store/private/types.go`

**Interfaces:**
- Produces: result field `SkipReason string` serialized as `skipReason,omitempty`.
- Produces: skip reasons `version-already-online` and `online-version-newer`.
- Consumes: `config.Config.Update.AllowDowngrade`, verified artifact version, and `storelookup.Result.OnlineVersion`.

- [ ] **Step 1: Write a failing public-flow test for a newer online official version**

Add a case that returns online `7.8.138` for candidate `7.7.406`, asserts neither authentication nor publishing runs, and expects `Skipped=true`, `OnlineVersion="7.8.138"`, and `SkipReason="online-version-newer"`.

- [ ] **Step 2: Run the focused test and verify RED**

Run: `go test ./internal/publishflow -run 'TestFlowSkipsOfficialPublishWhenOnlineVersionIsNewer' -count=1`

Expected: FAIL because the current flow publishes every different version and result structs lack `SkipReason`.

- [ ] **Step 3: Implement the minimal SemVer-aware decision**

Add `SkipReason` to both store results. Replace the boolean reconciliation result with a reason string. Exact equality returns `version-already-online`; valid online SemVer greater than valid candidate SemVer returns `online-version-newer` only when `AllowDowngrade` is false; otherwise return no skip. Normalize an optional leading `v` through the SemVer library without altering the reported versions.

- [ ] **Step 4: Run the focused test and verify GREEN**

Run: `go test ./internal/publishflow -run 'TestFlowSkipsOfficialPublishWhenOnlineVersionIsNewer' -count=1`

Expected: PASS.

- [ ] **Step 5: Add the remaining behavior matrix one case at a time**

Cover equal versions, candidate newer, explicit `allow_downgrade: true`, invalid online SemVer, invalid candidate SemVer, and private-store newer-online results. Each case must assert whether publisher/authentication dependencies were called and the exact skip reason.

- [ ] **Step 6: Run publish-flow tests**

Run: `go test ./internal/publishflow -count=1`

Expected: PASS.

- [ ] **Step 7: Commit the behavior**

```bash
git add internal/publishflow/flow.go internal/publishflow/flow_test.go internal/store/official/publish.go internal/store/private/types.go
git commit -m "fix: skip store downgrades before publishing"
```

### Task 2: Documentation and Skill contract

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `skills/lazycat-github-action/SKILL.md`
- Modify: `skills/lazycat-github-action/references/configuration.md`
- Modify: `skills/lazycat-github-action/references/workflows.md`
- Modify: `skills/lazycat-github-action/test-prompts.json`
- Modify: `internal/metadata/skill_test.go`

**Interfaces:**
- Documents: `skipReason`, SemVer ordering, `allow_downgrade`, and non-SemVer fallback.

- [ ] **Step 1: Add a failing metadata assertion**

Require the docs and Skill contract to include `online-version-newer`, `version-already-online`, `allow_downgrade: false`, and the rule that non-SemVer values only use exact equality.

- [ ] **Step 2: Run the metadata test and verify RED**

Run: `go test ./internal/metadata -run 'TestSkill' -count=1`

Expected: FAIL because the new contract is undocumented.

- [ ] **Step 3: Update English, Chinese, Skill, references, and evaluation prompt**

Document that newer online store versions are independently skipped before credentials are resolved, while explicit rollback authorization continues publishing. Add a troubleshooting/evaluation example based on EasyNVR `7.8.138 > 7.7.406` without naming or exposing credentials.

- [ ] **Step 4: Run metadata tests and verify GREEN**

Run: `go test ./internal/metadata -count=1`

Expected: PASS.

- [ ] **Step 5: Commit documentation**

```bash
git add README.md README.zh-CN.md skills/lazycat-github-action internal/metadata/skill_test.go
git commit -m "docs: explain store downgrade reconciliation"
```

### Task 3: Verification, release, and EasyNVR proof

**Files:**
- Modify only if required by release metadata checks.

**Interfaces:**
- Consumes: repository release workflow and EasyNVR reusable workflow using `@v1`.

- [ ] **Step 1: Run formatting and focused checks**

Run: `gofmt -w internal/publishflow/flow.go internal/publishflow/flow_test.go internal/store/official/publish.go internal/store/private/types.go`

Run: `go test ./internal/publishflow ./internal/metadata -count=1`

Expected: PASS.

- [ ] **Step 2: Run full verification**

Run: `go test ./... -count=1`

Run: `go test -race ./... -count=1`

Run: `go vet ./...`

Expected: all commands PASS.

- [ ] **Step 3: Run Darwin Skill dry-run against the updated Skill**

Follow `darwin-skill/SKILL.md` without spawning agents; apply only validation-backed corrections and rerun metadata tests if files change.

- [ ] **Step 4: Push and publish the next Action patch release**

Push `main`, trigger the repository release workflow, wait for success, and confirm both the immutable version tag and moving `v1` tag point to the release commit.

- [ ] **Step 5: Re-run EasyNVR**

Trigger `lazycat-contrib/easy-nvr-lzcapp` with `gh workflow run`. Confirm Release reconciliation and private-store handling succeed, official publishing logs `online_version=7.8.138`, `candidate_version=7.7.406`, and `skip_reason=online-version-newer`, and the overall workflow is green.

- [ ] **Step 6: Record verification evidence**

Report the release version, tags, commits, test gates, EasyNVR run URL, and per-store outcomes without printing secrets.
