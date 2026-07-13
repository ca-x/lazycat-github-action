# Official Store Backoff Retry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add opt-in, context-cancellable full-jitter retries for transient LazyCat official-store publication failures while preserving the current one-attempt default and safe error contract.

**Architecture:** Extend the strict project YAML with a validated official-store retry policy, pass that policy and the publish logger through `internal/publishflow`, and retry safe official publication operations after credential resolution. Retries before review repeat application existence handling and reopen the LPK; ambiguous review network/5xx outcomes are not replayed because review creation is non-idempotent. Cloudflare backoff supplies the jittered delay, an injected delay/wait seam keeps tests deterministic, and a response recorder carries safe `Retry-After` timing without exposing response bodies or credentials.

**Tech Stack:** Go 1.25, `github.com/cloudflare/backoff`, `log/slog`, `net/http`, strict YAML decoding, Go tests and race detector.

## Global Constraints

- Existing configurations retain one attempt and no added delay because retry defaults to `enabled: false`.
- `max_attempts` defaults to `3` and is valid from `2` through `10` when retry is enabled.
- `initial_delay` defaults to `2s` and is valid from `100ms` through `1m` when retry is enabled.
- `max_delay` defaults to `30s`, must be at least `initial_delay`, and must not exceed `5m` when retry is enabled.
- Retry only sanitized `*lpkgo.Error` values marked retryable, status-less `REMOTE_UNAVAILABLE`, HTTP `429`, or HTTP `5xx`; for `store.official.review`, retry only HTTP `429` and never replay status-less/5xx ambiguous outcomes. Never retry cancellation, deadline expiry, invalid configuration, authentication, permission, not-found, local-file, metadata-integrity, or other `4xx` failures.
- Resolve official credentials once before the retry loop; every retry before review must re-check application existence and reopen/re-stream the LPK.
- Use one `github.com/cloudflare/backoff.Backoff` per publication call with full jitter. A valid `Retry-After` delay wins only when greater than jitter and is capped at `max_delay`.
- Retry waits and active HTTP requests must stop immediately on context cancellation.
- Retry warnings may contain only store, completed attempt, maximum attempts, selected delay, safe error code, and optional HTTP status. Never log credentials or upstream response bodies.
- Preserve existing result JSON and final error wrapping. Exhaustion returns the final sanitized publication error.
- The fully populated retry object appears in documentation; the starter Skill asset contains only `retry.enabled: false`.
- Target Go 1.25; do not use Go 1.26+ APIs.

---

### Task 1: Add and validate the official retry configuration

**Files:**
- Modify: `internal/config/types.go`
- Modify: `internal/config/load.go`
- Modify: `internal/config/load_test.go`

**Interfaces:**
- Produces: `config.OfficialRetry{Enabled bool, MaxAttempts int, InitialDelay time.Duration, MaxDelay time.Duration}` at `Config.Stores.Official.Retry`.
- Consumes: strict YAML keys `enabled`, `max_attempts`, `initial_delay`, and `max_delay` under `stores.official.retry`.

- [ ] **Step 1: Write failing configuration tests**

Add table coverage proving: omitted retry defaults to disabled/3/2s/30s; explicit values are retained; malformed duration strings fail YAML decoding; enabled policies reject attempts below 2 or above 10, initial delay below 100ms or above 1m, max delay below initial delay, and max delay above 5m; disabled compatibility remains valid; unknown retry fields fail strict decoding.

- [ ] **Step 2: Run the focused test and confirm failure**

Run: `go test ./internal/config -run 'TestLoad'`

Expected: FAIL because `OfficialStore` has no retry contract/defaults.

- [ ] **Step 3: Add the configuration types and defaults**

Add `Retry OfficialRetry \`yaml:"retry"\`` to `OfficialStore`, import `time`, and define:

```go
type OfficialRetry struct {
	Enabled      bool          `yaml:"enabled"`
	MaxAttempts  int           `yaml:"max_attempts"`
	InitialDelay time.Duration `yaml:"initial_delay"`
	MaxDelay     time.Duration `yaml:"max_delay"`
}
```

In `applyDefaults`, set zero values to `3`, `2*time.Second`, and `30*time.Second`. In `validate`, apply the documented ranges only when retry is enabled; duration syntax remains strict at YAML decode time.

- [ ] **Step 4: Verify and commit the configuration contract**

Run:

```bash
gofmt -w internal/config/types.go internal/config/load.go internal/config/load_test.go
go test ./internal/config
git diff --check
git add internal/config/types.go internal/config/load.go internal/config/load_test.go
git commit -m "feat: configure official store retries"
```

Expected: tests pass and the commit contains only the retry configuration contract.

---

### Task 2: Retry safe official publication operations

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `internal/store/official/publish.go`
- Modify: `internal/store/official/publish_test.go`

**Interfaces:**
- Consumes: `official.Request.Retry config.OfficialRetry` and optional `official.Request.Logger *slog.Logger`.
- Produces: unchanged `Publisher.Publish(context.Context, Request) (Result, error)` behavior when retry is disabled.
- Test seam: `Publisher.NewDelay func(max, initial time.Duration) func() time.Duration` and `Publisher.Wait func(context.Context, time.Duration) error`.

- [ ] **Step 1: Add the Cloudflare dependency**

Run: `go get github.com/cloudflare/backoff@v0.0.0-20240920015135-e46b80a3a7d0`

Expected: `go.mod` and `go.sum` record the reviewed Cloudflare package without unrelated upgrades.

- [ ] **Step 2: Write failing retry tests**

Add deterministic tests covering:

- default-off HTTP 503 performs exactly one complete attempt;
- enabled retry repeats the existence check and succeeds after a transient upload 503;
- HTTP 400 fails once even when enabled;
- exhausted 503 attempts return the final `*lpkgo.Error` and exact attempt count;
- token resolution occurs once across retries;
- cancellation during an injected wait returns promptly without starting another attempt;
- a larger valid `Retry-After` delay is selected and capped by `max_delay`;
- a structured warning contains only the documented safe fields;
- upload/review HTTP 429 and 5xx errors are marked retryable at source.
- review HTTP 429 may retry, while review status-less/5xx outcomes are not replayed by the operation classifier.

Use `NewDelay` and `Wait` injections so tests do not sleep and do not depend on random jitter.

- [ ] **Step 3: Run the focused tests and confirm failure**

Run: `go test ./internal/store/official -run 'TestPublisher.*Retry|TestPublisher.*Attempt|TestPublisher.*Cancellation'`

Expected: FAIL because request retry state and the retry loop do not exist.

- [ ] **Step 4: Refactor one publication attempt behind the retry loop**

Keep request validation, changelog/application preparation, token resolution, base URL selection, and client creation before the loop. Move application check/create, upload metadata verification, and review submission into a helper that performs one operation attempt. For a retryable failure before review, call that helper again so it reopens the LPK through `uploadLPK`. Do not replay `store.official.review` after a status-less network failure or 5xx; HTTP 429 remains retryable.

Create one default delay closure per call:

```go
policy := backoff.New(request.Retry.MaxDelay, request.Retry.InitialDelay)
nextDelay := policy.Duration
```

When `Publisher.NewDelay` is set, use its returned closure instead. When `Publisher.Wait` is nil, use a timer/select helper that returns on `ctx.Done()`.

- [ ] **Step 5: Add exact retry classification and Retry-After handling**

Classify through `errors.As(err, *lpkgo.Error)`, rejecting cancellation/deadline codes first, then accepting `Retryable`, status-less `REMOTE_UNAVAILABLE`, HTTP 429, or 5xx. For `store.official.review`, accept only HTTP 429 because network/5xx outcomes may already have created a pending review. Mark 429/5xx retryable at the HTTP source while keeping metadata-integrity failures non-retryable.

Wrap the HTTP transport with a per-publication recorder that reads only the `Retry-After` header from 429/5xx responses. Accept non-negative delta-seconds or `http.ParseTime` dates, keep the greatest delay seen in the failed attempt, choose `max(jitter, retryAfter)`, and cap it at `request.Retry.MaxDelay`.

Before waiting, emit:

```go
logger.Warn("official store publication retry scheduled",
	"store", "official",
	"attempt", attempt,
	"max_attempts", request.Retry.MaxAttempts,
	"delay", delay,
	"code", toolkitError.Code,
)
```

Append `status` only when non-zero.

- [ ] **Step 6: Verify official publishing and commit**

Run:

```bash
gofmt -w internal/store/official/publish.go internal/store/official/publish_test.go
go test ./internal/store/official
go test -race ./internal/store/official -count=10
git diff --check
git add go.mod go.sum internal/store/official/publish.go internal/store/official/publish_test.go
git commit -m "feat: retry transient official publishing failures"
```

Expected: all official-store tests pass without real waits, races, response-body logging, or token re-resolution.

---

### Task 3: Pass retry policy and logging through the publish flow

**Files:**
- Modify: `internal/publishflow/flow.go`
- Modify: `internal/publishflow/flow_test.go`

**Interfaces:**
- Consumes: `request.Config.Stores.Official.Retry` and the normalized `Flow.Logger`.
- Produces: `official.Request.Retry` and `official.Request.Logger` for real official publication; dry-run remains network-free.

- [ ] **Step 1: Write a failing publish-flow propagation test**

Extend the existing official publish test with explicit retry values and assert the captured `official.Request` contains the exact enabled/max-attempt/initial/max policy and the same logger used by the flow.

- [ ] **Step 2: Run the focused test and confirm failure**

Run: `go test ./internal/publishflow -run TestPublishOfficial`

Expected: FAIL because the official request does not carry retry policy or logger.

- [ ] **Step 3: Pass the validated policy and logger**

Pass the logger already selected in `Flow.Publish` into `publishOfficial`, then populate:

```go
Retry:  request.Config.Stores.Official.Retry,
Logger: logger,
```

Do not move credential resolution into the official retry loop and do not add Action or workflow inputs.

- [ ] **Step 4: Verify and commit flow integration**

Run:

```bash
gofmt -w internal/publishflow/flow.go internal/publishflow/flow_test.go
go test ./internal/publishflow ./internal/action
git diff --check
git add internal/publishflow/flow.go internal/publishflow/flow_test.go
git commit -m "feat: pass official retry policy to publishing"
```

Expected: flow/action tests pass and public result JSON is unchanged.

---

### Task 4: Keep non-official compatibility warnings non-blocking

**Files:**
- Modify: `internal/build/build.go`
- Modify: `internal/build/build_test.go`
- Modify: `internal/action/action.go`
- Modify: `internal/action/action_test.go`
- Modify: `internal/publishflow/flow.go`
- Modify: `internal/publishflow/flow_test.go`
- Create: `internal/store/official/precheck.go`
- Create: `internal/store/official/precheck_test.go`
- Modify: `internal/store/official/publish.go`

**Interfaces:**
- Consumes: `lint.Package(..., lint.WithOfficial())` and `lint.IsOfficialWarning(lpkgo.Warning)`.
- Produces: shared builds that retain all warnings without blocking private publication, plus an official-only precheck that fails only the official store on official/developer-platform warnings.

- [ ] **Step 1: Write a failing `container_name` regression test**

Build an otherwise official-compatible LPK through `Builder.Build` with `Official: true` and `FailOnWarnings: true`, and add `services.web.container_name`. Assert the build succeeds, the final LPK exists, and the result contains `unknown-manifest-fields` at `services.web.container_name`. Add an Action test proving a dual-store build requests official lint but does not set `FailOnWarnings`, and an official publisher test proving official warnings fail before token/network while compatibility warnings still publish.

- [ ] **Step 2: Run the focused test and confirm failure**

Run: `go test ./internal/build ./internal/action ./internal/store/official -run 'TestBuilderOfficialAllowsCompatibilityWarnings|TestRunBuildKeepsOfficialWarningsStoreScoped|TestPublisherOfficialPrecheck'`

Expected: FAIL because the Action currently promotes every lint warning to a shared build failure and the official publisher has no store-local precheck.

- [ ] **Step 3: Filter only blocking official warnings**

Keep all lint warnings in `Result.Warnings`. When `request.Official` is true, construct the fail-on-warning set from warnings where `lint.IsOfficialWarning(warning)` is true; when `request.Official` is false, preserve the existing behavior of treating all warnings as blocking when `FailOnWarnings` is requested. Do not add `container_name` to the toolkit schema and do not suppress the warning.

In the Action's shared build/check calls, keep `Official: cfg.Stores.Official.Enabled` so official warnings are collected, but do not set `FailOnWarnings`; this lets the verified LPK reach the private-first store sequence. Add `official.PrecheckFile(ctx, lpkPath)` that opens the archive with conservative named limits and materializes only bounded lint metadata (`manifest.yml`, optional `package.yml`, bounded `icon.png`, optional bounded `images.lock`, any non-directory `devshell` marker, referenced blob existence placeholders, and resource-export structure). It must never expand unrelated payload or image blob bytes. Run `lint.Package(..., lint.WithOfficial())`, filter with `lint.IsOfficialWarning`, and return a sanitized `INVALID_MANIFEST` error only when official warnings remain; map unsafe/invalid archive structure to invalid LPK while preserving cancellation, missing-file, and operational I/O classifications.

Invoke the precheck in `publishflow.Flow.Publish` for `TargetOfficial` after any anonymous `skip_if_version_exists` lookup has decided publication is still necessary, but before dry-run return, credential resolution, or publication. Keep the same precheck at the start of `official.Publisher.Publish` as defense in depth for direct callers. An equal or newer online version skips without redundant official lint blocking.

- [ ] **Step 4: Verify and commit the lint severity fix**

Run:

```bash
gofmt -w internal/build/build.go internal/build/build_test.go internal/action/action.go internal/action/action_test.go internal/publishflow/flow.go internal/publishflow/flow_test.go internal/store/official/precheck.go internal/store/official/precheck_test.go internal/store/official/publish.go
go test ./internal/build ./internal/action ./internal/publishflow ./internal/store/official
go test ./...
git diff --check
git add internal/build/build.go internal/build/build_test.go internal/action/action.go internal/action/action_test.go internal/publishflow/flow.go internal/publishflow/flow_test.go internal/store/official/precheck.go internal/store/official/precheck_test.go internal/store/official/publish.go
git commit -m "fix: keep compatibility lint warnings non-blocking"
```

Expected: private-only and dual-store shared builds complete with warnings, private publishing remains reachable, official publication alone fails on official warnings before credentials/network, and `container_name` remains visible without blocking either store.

---

### Task 5: Isolate and explain official-store failures in dual-store workflows

**Files:**
- Modify: `.github/workflows/lazycat.yml`
- Modify: `internal/metadata/action_test.go`
- Modify: `internal/store/official/publish.go`
- Modify: `internal/store/official/publish_internal_test.go`
- Modify: `internal/action/action.go`
- Modify: `internal/action/action_test.go`

**Interfaces:**
- Consumes: `steps.lazycat.outputs.official-store-enabled`, `steps.lazycat.outputs.private-store-enabled`, and each publishing step's GitHub `outcome`.
- Produces: independent store attempts, a non-fatal official failure when the private store is also configured, a merged failure marker in `store-results`, and a bounded public message extracted from recognized official JSON errors.

- [ ] **Step 1: Write failing reusable-workflow contract tests**

Require the official publishing step to run after a private-store failure unless the job was cancelled, and to use conditional `continue-on-error` only when the private store is configured. Require the merge step to run after tolerated failures and to record an official failure object without exposing response bodies or credentials. Add response-message tests for top-level `message`/`msg`, string or nested `error`, whitespace normalization, length bounds, non-JSON suppression, and credential-marker suppression.

- [ ] **Step 2: Run the metadata test and confirm failure**

Run: `go test ./internal/metadata -run 'TestReusableWorkflow'`

Expected: FAIL because the official step currently inherits the job's success guard, every official failure is fatal, and the merge step has no partial-failure result.

- [ ] **Step 3: Make store publication outcomes independent**

Add an explicit `!cancelled()` status guard to the official publish condition so a private-store failure does not suppress the official attempt. Set `continue-on-error` on the official step only when the private store is enabled. Keep official-only configurations strict: an official failure remains a failed job when there is no private target.

Run the merge step with `always() && !cancelled()`. When the official step outcome is `failure`, add this safe result shape before merging successful store outputs:

```json
{"official":{"published":false,"failed":true,"failureReason":"official-publish-failed"}}
```

Emit a GitHub warning and step summary explaining that private publication completed independently and the official submission needs attention. Do not include upstream response bodies, tokens, request payloads, or credentials.

For rejected official HTTP responses, inspect only valid JSON and extract the first recognized string from `message`, `msg`, string `error`, or nested `error.message`/`error.msg`. Normalize it to one line, bound it to 512 bytes, suppress messages containing credential markers, and expose it through an explicit public-detail marker. Keep arbitrary cause text hidden. The final sanitized error may append `message="..."` after status and operation.

- [ ] **Step 4: Verify and commit workflow isolation**

Run:

```bash
gofmt -w internal/metadata/action_test.go internal/store/official/publish.go internal/store/official/publish_internal_test.go internal/action/action.go internal/action/action_test.go
go test ./internal/metadata ./internal/store/official ./internal/action
bash scripts/run-action_test.sh
git diff --check
git add .github/workflows/lazycat.yml internal/metadata/action_test.go internal/store/official/publish.go internal/store/official/publish_internal_test.go internal/action/action.go internal/action/action_test.go
git commit -m "fix: isolate official store publication failures"
```

Expected: dual-store official failures remain visible but do not fail the job, private failures do not suppress the official attempt, and official-only failures remain fatal.

---

### Task 6: Document, teach, verify, and release the feature

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `skills/lazycat-github-action/SKILL.md`
- Modify: `skills/lazycat-github-action/references/configuration.md`
- Modify: `skills/lazycat-github-action/references/workflows.md`
- Modify: `skills/lazycat-github-action/assets/lazycat-action.yml`
- Modify: `skills/lazycat-github-action/test-prompts.json`
- Modify: `skills/lazycat-github-action/evals/evals.json`
- Modify: `internal/metadata/skill_test.go`
- Modify: `action.yml`

**Interfaces:**
- Produces: precise English/Chinese configuration guidance, starter configuration, Skill behavior, metadata/eval coverage, and Action bootstrap version `v1.1.15`.

- [ ] **Step 1: Add failing Skill/documentation contract tests**

Require all contract documents to mention `retry`, `enabled: false`, `max_attempts`, `initial_delay`, `max_delay`, retryable network/429/5xx scope, non-retryable auth/other-4xx scope, context cancellation, and the no-response-body/no-secret warning boundary. Require the starter asset to contain only:

```yaml
retry:
  enabled: false
```

Add one test prompt and one eval for configuring default-off official retries; update the prompt count from 13 to 14.

- [ ] **Step 2: Run metadata tests and confirm failure**

Run: `go test ./internal/metadata`

Expected: FAIL until documentation, assets, prompt metadata, and version bootstrap are updated.

- [ ] **Step 3: Update documentation and Skill contracts**

Document the full example:

```yaml
retry:
  enabled: false
  max_attempts: 3
  initial_delay: 2s
  max_delay: 30s
```

State that attempts include the initial request; enabled range is 2-10; durations use Go syntax; safe upload/check failures may retry connection/TLS/timeout/reset, HTTP 429, and 5xx; review retries only HTTP 429 because ambiguous network/5xx outcomes must not be replayed; retries before review reopen the LPK and repeat the application existence check; credentials are resolved once; cancellation interrupts requests and waits; final errors/results stay compatible; warnings never include credentials or response bodies. Also document that general compatibility warnings such as unknown `container_name` remain visible but do not block an official build; only warnings classified by `lint.IsOfficialWarning` are fatal for official publication.

- [ ] **Step 4: Bump the Action bootstrap patch version**

Change `LAZYCAT_ACTION_VERSION` in `action.yml` from `v1.1.14` to `v1.1.15`. Do not change Action inputs, reusable-workflow inputs, or output schemas.

- [ ] **Step 5: Run complete verification**

Run:

```bash
gofmt -w internal/config/types.go internal/config/load.go internal/config/load_test.go internal/store/official/publish.go internal/store/official/publish_test.go internal/publishflow/flow.go internal/publishflow/flow_test.go internal/build/build.go internal/build/build_test.go internal/metadata/skill_test.go
git diff --check
go test ./...
go test -race ./...
go vet ./...
staticcheck ./...
bash scripts/run-action_test.sh
```

Expected: every command exits 0 with no race, vet, Staticcheck, formatting, integration, metadata, or secret-boundary failure.

- [ ] **Step 6: Commit documentation and release metadata**

Run:

```bash
git add README.md README.zh-CN.md skills/lazycat-github-action internal/metadata/skill_test.go action.yml docs/superpowers/plans/2026-07-13-official-store-backoff-retry.md
git commit -m "docs: explain official store retry policy"
```

- [ ] **Step 7: Final branch review and publication**

Review the complete range from `a444224` to `HEAD`, fix all Critical/Important findings, rerun the complete verification suite, merge the feature branch into `main`, push `main`, create and push tag `v1.1.15`, wait for the release workflow, update floating tag `v1` to the verified release commit, and verify the published release plus a consumer workflow with retry explicitly enabled.
