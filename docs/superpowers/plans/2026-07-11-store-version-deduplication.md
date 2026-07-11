# Store Version Deduplication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade to `lzc-toolkit-go v0.2.0`, optionally skip official or Miaomiao publishing when the exact package's latest visible version equals the verified LPK version, support optional private group codes through a GitHub Secret, update all documentation and the repository Skill, optimize that Skill with Darwin, and release `v1.1.0`.

**Architecture:** Keep anonymous read clients separate from authenticated write clients. `publishflow` performs an opt-in lookup after local artifact verification and before credential resolution, treats not-found as permission to publish, fails closed on other lookup errors, and returns an additive skipped result on exact version equality. Group codes enter only through `PRIVATE_STORE_GROUP_CODES` and are handed to the toolkit private client.

**Tech Stack:** Go 1.25+, `github.com/lib-x/lzc-toolkit-go v0.2.0`, YAML v3, GitHub composite Actions and reusable workflows, GoReleaser 2, actionlint, ShellCheck, Darwin Skill 2.0.

## Global Constraints

- Existing configuration behavior remains unchanged unless `skip_if_version_exists` is explicitly `true` for a store.
- Version comparison is trimmed exact string equality; do not compare hashes, changelogs, timestamps, or ordering.
- `dry-run` remains network-free and never performs a store lookup.
- Only not-found permits fallback from lookup to publishing; every other lookup failure stops the operation.
- `PRIVATE_STORE_GROUP_CODES` is a secret/environment variable, never a YAML field, Action input, output, result field, summary value, or log value.
- Official and private anonymous lookup clients must never receive publishing tokens.
- `README.md`, `README.zh-CN.md`, Skill references/assets/evals, and public metadata must remain synchronized.
- Publish immutable `v1.1.0` and verify it before moving floating tag `v1`.

---

### Task 1: Upgrade the toolkit and extend configuration

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `internal/version/version.go`
- Modify: `internal/version/version_test.go`
- Modify: `internal/config/types.go`
- Modify: `internal/config/load.go`
- Modify: `internal/config/load_test.go`

**Interfaces:**
- Produces: `OfficialStore.SkipIfVersionExists bool` and `PrivateStore.SkipIfVersionExists bool`, decoded from `skip_if_version_exists`.
- Produces: version metadata reporting toolkit `v0.2.0`.

- [ ] **Step 1: Write failing dependency/configuration tests**

Add assertions that omitted flags remain false, explicit flags decode true, unknown near-miss fields remain rejected by `KnownFields`, and `version.Info().ToolkitVersion == "v0.2.0"`.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/config ./internal/version`

Expected: FAIL because the new fields and toolkit version are not implemented.

- [ ] **Step 3: Add the two additive YAML fields and upgrade the dependency**

Add exactly:

```go
type OfficialStore struct {
    Enabled             bool                `yaml:"enabled"`
    SkipIfVersionExists bool                `yaml:"skip_if_version_exists"`
    CreateIfMissing     bool                `yaml:"create_if_missing"`
    Locales             []string            `yaml:"changelog_locales"`
    Application         OfficialApplication `yaml:"application"`
}

type PrivateStore struct {
    Enabled             bool   `yaml:"enabled"`
    SkipIfVersionExists bool   `yaml:"skip_if_version_exists"`
    Name                string `yaml:"name"`
    Summary             string `yaml:"summary"`
}
```

Run `go get github.com/lib-x/lzc-toolkit-go@v0.2.0`, update `ToolkitVersion`, then `go mod tidy`.

- [ ] **Step 4: Run focused tests and dependency verification**

Run: `go test ./internal/config ./internal/version && go mod verify`

Expected: PASS and `all modules verified`.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/config internal/version
git commit -m "feat: configure store version deduplication"
```

### Task 2: Add anonymous latest-version lookup adapters

**Files:**
- Create: `internal/storelookup/lookup.go`
- Create: `internal/storelookup/lookup_test.go`

**Interfaces:**
- Produces: `type Request struct { Store Store; PackageID, BaseURL string; GroupCodes []string; HTTPClient *http.Client }`.
- Produces: `type Result struct { OnlineVersion string }`.
- Produces: `type Lookup func(context.Context, Request) (Result, error)`.
- Produces: `func Default(context.Context, Request) (Result, error)` using toolkit official/private packages.

- [ ] **Step 1: Write failing adapter tests**

Use `httptest.Server` fixtures matching the toolkit `official.Application` and `private.LatestVersion` response contracts. Assert exact package identity, returned version, comma-split group codes reaching `X-Group-Codes`, no Cookie forwarding, no redirect following, not-found preservation through `errors.Is(err, lpkgo.ErrNotFound)`, and invalid store rejection.

- [ ] **Step 2: Run the focused tests and confirm failure**

Run: `go test ./internal/storelookup`

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement the narrow adapter**

Define stores `official` and `private`. For official, construct `official.New(official.Options{MetadataBaseURL: request.BaseURL, HTTPClient: request.HTTPClient})` and return `application.Version.Name`; an empty `BaseURL` selects the toolkit default. For private, construct `private.New(private.Options{BaseURL: request.BaseURL, GroupCodes: request.GroupCodes, HTTPClient: request.HTTPClient})` and return `latest.LatestVersion.Version`. Trim the returned version and reject an empty value.

Do not accept or forward authentication tokens. Let toolkit error codes pass through unchanged.

- [ ] **Step 4: Run adapter tests**

Run: `go test ./internal/storelookup`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storelookup
git commit -m "feat: query latest store versions"
```

### Task 3: Integrate fail-closed deduplication into publishing

**Files:**
- Modify: `internal/store/official/publish.go`
- Modify: `internal/store/private/types.go`
- Modify: `internal/publishflow/flow.go`
- Modify: `internal/publishflow/flow_test.go`
- Modify: `internal/action/action_test.go`

**Interfaces:**
- `publishflow.Flow` gains `LookupVersion storelookup.Lookup`.
- Official/private result objects gain `Skipped bool \`json:"skipped"\`` and `OnlineVersion string \`json:"onlineVersion,omitempty"\``.
- Lookup group codes come from comma-splitting `PRIVATE_STORE_GROUP_CODES` with whitespace trimming; toolkit owns code validation/deduplication.

- [ ] **Step 1: Write failing publish-flow tests**

Cover this table for each store:

| Flag | Lookup result | Expected |
|---|---|---|
| false | lookup would fail | no lookup, normal publish |
| true | same version | success, `published=false`, `skipped=true`, no auth/write call |
| true | different version | normal publish with `onlineVersion` recorded |
| true | not found | normal publish |
| true | other error | operation fails, no auth/write call |
| true + dry-run | lookup would fail | no lookup and no write |

For private lookup, assert `APPSTORE_URL` and parsed `PRIVATE_STORE_GROUP_CODES` arrive in the lookup request while `APPSTORE_TOKEN` does not.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/publishflow ./internal/action`

Expected: FAIL because lookup and result fields are absent.

- [ ] **Step 3: Implement the decision before credential resolution**

After artifact verification and before `publishOfficial`/`publishPrivate` authentication, call a helper equivalent to:

```go
func (flow Flow) checkExisting(ctx context.Context, request Request, artifact lpkcheck.Result) (string, bool, error)
```

Return `(onlineVersion, true, nil)` only for exact equality. Treat `lpkgo.ErrNotFound` as `(zero, false, nil)`. Wrap all other errors with a safe store-lookup message. Skip the helper when disabled or dry-run.

Populate skipped store results without invoking `ResolveAuth`, `PublishOfficial`, `NewPrivate`, or private write credentials.

- [ ] **Step 4: Preserve online version on normal publication**

When lookup succeeds with a different version, carry `OnlineVersion` into the final store result after the write call. Keep existing publishing fields and JSON keys unchanged.

- [ ] **Step 5: Run focused and race tests**

Run: `go test ./internal/publishflow ./internal/action ./internal/store/... && go test -race ./internal/publishflow ./internal/action ./internal/store/...`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/publishflow internal/action/action_test.go internal/store
git commit -m "feat: skip duplicate store submissions"
```

### Task 4: Wire the group-code Secret and public Action metadata

**Files:**
- Modify: `.github/workflows/lazycat.yml`
- Modify: `internal/metadata/action_test.go`
- Modify: `internal/build/build.go`
- Modify: `internal/build/build_test.go`

**Interfaces:**
- Reusable workflow secret: `PRIVATE_STORE_GROUP_CODES`, optional.
- Composite Action has no `private-group-codes` input.
- Private publish step maps the secret to environment variable `PRIVATE_STORE_GROUP_CODES`.

- [ ] **Step 1: Write failing metadata and secret-isolation tests**

Require `PRIVATE_STORE_GROUP_CODES` in reusable-workflow secrets and private publish environment. Assert it is absent from `action.yml` inputs. Add it to buildscript protected-environment tests so untrusted project build scripts cannot inherit it.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/metadata ./internal/build`

Expected: FAIL because the secret is not declared or protected.

- [ ] **Step 3: Add the secret wiring and build boundary**

Declare:

```yaml
secrets:
  PRIVATE_STORE_GROUP_CODES:
    required: false
```

Map it only on the private publish step:

```yaml
env:
  PRIVATE_STORE_GROUP_CODES: ${{ secrets.PRIVATE_STORE_GROUP_CODES }}
```

Add the environment key to `protectedEnvironment` in the LPK build runner.

- [ ] **Step 4: Run focused tests and actionlint**

Run: `go test ./internal/metadata ./internal/build && go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/*.yml examples/*/.github/workflows/*.yml`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/lazycat.yml internal/metadata internal/build
git commit -m "feat: pass private store group codes securely"
```

### Task 5: Update bilingual documentation, examples, and repository Skill

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `skills/lazycat-github-action/SKILL.md`
- Modify: `skills/lazycat-github-action/references/configuration.md`
- Modify: `skills/lazycat-github-action/references/workflows.md`
- Modify: `skills/lazycat-github-action/assets/lazycat-action.yml`
- Modify: `skills/lazycat-github-action/assets/lazycat-workflow.yml`
- Modify: `skills/lazycat-github-action/evals/evals.json`
- Modify: `internal/metadata/skill_test.go`

**Interfaces:**
- Documentation exposes both `skip_if_version_exists` flags, exact equality, fail-closed errors, not-found continuation, network-free dry-run, skipped result fields, and `PRIVATE_STORE_GROUP_CODES` Secret usage.

- [ ] **Step 1: Write failing Skill metadata/evaluation assertions**

Require the Skill and references to contain `skip_if_version_exists`, `PRIVATE_STORE_GROUP_CODES`, `onlineVersion`, and the rule that group codes are secrets. Add an eval prompt for a repository that publishes to both stores and must avoid equal-version resubmission.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/metadata`

Expected: FAIL because documentation is not synchronized.

- [ ] **Step 3: Update README and Skill content**

Add runnable English and Chinese YAML/workflow examples, the secret declaration/caller binding, result JSON examples, and troubleshooting entries. Update all Skill assets so generated configuration and workflows use the same contract.

- [ ] **Step 4: Update toolkit and release version references**

Replace public compatibility text from toolkit `v0.1.0` to `v0.2.0`. Do not change the lzc-cli compatibility baseline unless the dependency documentation requires it.

- [ ] **Step 5: Validate docs and Skill**

Run:

```bash
go test ./internal/metadata
rg -n "v0\.1\.0|PRIVATE_STORE_GROUP_CODES|skip_if_version_exists|onlineVersion" README.md README.zh-CN.md skills/lazycat-github-action internal/version
git diff --check
```

Expected: metadata tests pass, no stale toolkit `v0.1.0` remains in public/version files, and the new contract appears in both READMEs and the Skill.

- [ ] **Step 6: Commit**

```bash
git add README.md README.zh-CN.md skills/lazycat-github-action internal/metadata/skill_test.go
git commit -m "docs: document store version deduplication"
```

### Task 6: Update Action version and run the complete functional gate

**Files:**
- Modify: `action.yml`
- Modify: `docs/superpowers/plans/2026-07-11-store-version-deduplication.md`

**Interfaces:**
- Composite bootstrap version becomes immutable `v1.1.0`.

- [ ] **Step 1: Update embedded Action version**

Set `LAZYCAT_ACTION_VERSION: v1.1.0` in `action.yml` and update metadata tests if they pin the value.

- [ ] **Step 2: Run the full local gate**

Run fresh:

```bash
go test -count=1 -race ./...
go vet ./...
bash -n scripts/run-action.sh scripts/run-action_test.sh examples/*/scripts/*.sh
bash scripts/run-action_test.sh
shellcheck scripts/*.sh testdata/*/scripts/*.sh examples/*/scripts/*.sh
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/*.yml examples/*/.github/workflows/*.yml
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/lazycat-action
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/lazycat-action
go run github.com/goreleaser/goreleaser/v2@v2.17.0 release --snapshot --clean --skip=publish,sbom
scripts/verify-release-artifacts.sh
git diff --check
```

Expected: every available command exits zero. If local ShellCheck is unavailable, record that exact local gap and require the current GitHub CI ShellCheck job before release.

- [ ] **Step 3: Run secret and dependency scans**

Run:

```bash
git grep -nE 'lcst_[A-Za-z0-9]|(LAZYCAT|APPSTORE|PRIVATE_STORE|LZC_CLI)_(TOKEN|PASSWORD|GROUP_CODES)[[:space:]]*[:=][[:space:]]*[^$<{]' -- ':!docs/**' ':!skills/**' ':!**/*_test.go'
go mod verify
```

Expected: no committed credential values and all modules verified.

- [ ] **Step 4: Commit functional release candidate**

```bash
git add action.yml docs/superpowers/plans/2026-07-11-store-version-deduplication.md
git commit -m "chore: prepare v1.1.0"
```

### Task 7: Run Darwin optimization on the repository Skill

**Files:**
- Modify: `skills/lazycat-github-action/SKILL.md`
- Create or modify: `skills/lazycat-github-action/test-prompts.json`
- Create or modify: Darwin `results.tsv` at the location required by the installed Darwin workflow

**Interfaces:**
- Consumes: functionally complete Skill from Task 5.
- Produces: user-approved Skill version whose Darwin score is strictly higher than baseline, or the unchanged baseline if no attempted edit improves it.

- [ ] **Step 1: Start the Darwin branch and runtime-neutrality scan**

Create `auto-optimize/20260711-lazycat-github-action`, initialize/inspect Darwin results, and run the required runtime red-light grep against the repository Skill and related README.

- [ ] **Step 2: Design two or three representative prompts**

Cover: dual-store deduplication with group-code Secret; an official-store-only image project; and an ambiguous private-store request where credentials must not be embedded. Save them to `test-prompts.json`.

- [ ] **Step 3: Stop for the Darwin prompt checkpoint**

Present all prompts and expected outcomes to the user. Do not score or modify the Skill until approved.

- [ ] **Step 4: Establish the nine-dimension baseline**

Use independent with-Skill and baseline executions for every prompt, independent judge agents for effect scoring, record all dimensions and evaluation mode, then present the baseline card.

- [ ] **Step 5: Stop for the Darwin baseline checkpoint**

Wait for user approval before optimization.

- [ ] **Step 6: Apply validation-gated optimization**

Change one dimension per round, commit each attempt, re-evaluate independently, retain only strict score improvements, and use `git revert` rather than destructive reset for regressions. Stop after two consecutive improvements below two points or the three-round maximum.

- [ ] **Step 7: Stop for the Darwin optimized-Skill checkpoint**

Show the before/after diff, dimension scores, prompt comparisons, and retained commits. Wait for explicit user approval; revert the optimization if rejected.

- [ ] **Step 8: Merge the approved optimization and revalidate**

Fast-forward or otherwise preserve the approved commits on `main`, then run `go test ./internal/metadata` and the repository Skill eval/validation command documented by the project.

### Task 8: Push, verify CI, publish `v1.1.0`, and move `v1`

**Files:**
- Modify: `docs/superpowers/plans/2026-07-11-store-version-deduplication.md` only for the final verification record.

**Interfaces:**
- Produces: pushed `main`, immutable annotated `v1.1.0`, verified GitHub Release, and annotated floating `v1` at the same release commit.

- [ ] **Step 1: Record final local state**

Run `git status --short --branch -uall`, `git rev-parse HEAD`, `git log --oneline v1.0.0..HEAD`, and confirm only intended commits/files exist.

- [ ] **Step 2: Push main and wait for current-HEAD CI**

Push `main`, locate the exact-head GitHub CI run, and require all test, shell, cross-build, release-contract, and fixture jobs to pass.

- [ ] **Step 3: Create and push immutable release tag**

Create annotated `v1.1.0` at the verified commit and push only that tag. Wait for the release workflow.

- [ ] **Step 4: Verify published artifacts**

Download `checksums.txt`, amd64/arm64 archives, and SBOMs. Verify checksums, archive entries, binary `--version` output (`v1.1.0`, toolkit `v0.2.0`, target `linux/amd64`), GitHub asset digests, QEMU arm64 smoke result, and provenance attestations.

- [ ] **Step 5: Move floating v1 only after immutable release verification**

Create/update annotated `v1` at the `v1.1.0` commit, push it, and re-read both remote tag objects to prove they resolve to the same commit.

- [ ] **Step 6: Record and commit release verification**

Update this plan with CI run ID, release URL, asset verification, tag commit, and remaining gaps. Commit and push the record, then confirm the final docs-only CI run passes.

## Functional Verification Record

- 2026-07-11: `go test -count=1 -race ./...`, `go vet ./...`, bootstrap tests, actionlint 1.7.7, Linux amd64/arm64 builds, `go mod verify`, and `git diff --check` passed after upgrading to `lzc-toolkit-go v0.2.0`.
- GoReleaser 2.17.0 snapshot release and `scripts/verify-release-artifacts.sh` passed; both archives contain only `lazycat-action`, and `dist/checksums.txt` verifies both archives.
- Local ShellCheck is unavailable in this environment. The exact-head GitHub CI ShellCheck job remains a mandatory release gate.
- Credential scan found only GitHub `${{ secrets.* }}` references and documented placeholder values; no concrete credential was committed.
- Exact-head CI run [29138296024](https://github.com/ca-x/lazycat-github-action/actions/runs/29138296024) passed at commit `008b3aacbdba6820132cbf158679961c5931c45e`: race tests, vet, actionlint, ShellCheck, Linux amd64/arm64 cross-builds, fixture LPK, and release-contract all succeeded.
- Release workflow [29138387767](https://github.com/ca-x/lazycat-github-action/actions/runs/29138387767) completed successfully, including GoReleaser, amd64/arm64 smoke tests, provenance attestation, and the annotated floating `v1` update.
- The non-draft, non-prerelease [`v1.1.0` Release](https://github.com/ca-x/lazycat-github-action/releases/tag/v1.1.0) contains `checksums.txt`, amd64/arm64 archives, and an SBOM for each archive. Fresh downloads passed `sha256sum -c checksums.txt`; each archive contains only `lazycat-action`.
- The downloaded amd64 binary reports Action `v1.1.0`, toolkit `v0.2.0`, and target `linux/amd64`, and `file` identifies it as a statically linked x86-64 ELF. `gh attestation verify` succeeded for both release archives.
- Remote annotated tags `v1.1.0` and `v1` both dereference to `008b3aacbdba6820132cbf158679961c5931c45e`.

## Darwin Verification Record

- The repository Skill was evaluated with four representative prompts and three real `lazycat-contrib` repositories: `new-api-lzcapp`, `lazycat-mcp`, and `apps-scheduler`.
- Baseline score was 80.2. The first accepted checkpoint improvement reached 82.8; an anti-pattern blacklist attempt was rejected by two judges and reverted.
- User feedback established automatic GitHub workflow creation as the Skill's primary outcome. Real-repository validation exposed and then fixed workflow-mode selection, LPK output-under-contentdir, implicit buildscript, and unnecessary Secret inheritance defects.
- Final Darwin score is 88.4. The Skill remains below 150% of its original size, runtime-neutrality scan is clean, and `go test ./internal/metadata` passes.
- Real-project evidence: all three generated Action/workflow pairs passed actionlint; both Exec projects built LPKs containing Linux x86-64 binaries; the multi-service Docker project manages only `new-api`, leaves Redis unmanaged, explicitly disables buildscript execution, and does not inherit unused Secrets.
- Darwin test prompts are committed at `skills/lazycat-github-action/test-prompts.json`; detailed experiment history is recorded in the installed Darwin `results.tsv`.
