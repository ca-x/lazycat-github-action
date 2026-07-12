# Official Store Backoff Retry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add opt-in, context-cancellable full-jitter retries for transient LazyCat official-store publication failures while preserving the current one-attempt default and safe error contract.

**Architecture:** Extend the strict project YAML with a validated official-store retry policy, pass that policy and the publish logger through `internal/publishflow`, and wrap the complete official publication attempt after credential resolution. Each retry repeats application existence handling and reopens the LPK; Cloudflare backoff supplies the jittered delay, an injected delay/wait seam keeps tests deterministic, and a response recorder carries safe `Retry-After` timing without exposing response bodies or credentials.

**Tech Stack:** Go 1.25, `github.com/cloudflare/backoff`, `log/slog`, `net/http`, strict YAML decoding, Go tests and race detector.

## Global Constraints

- Existing configurations retain one attempt and no added delay because retry defaults to `enabled: false`.
- `max_attempts` defaults to `3` and is valid from `2` through `10` when retry is enabled.
- `initial_delay` defaults to `2s` and is valid from `100ms` through `1m` when retry is enabled.
- `max_delay` defaults to `30s`, must be at least `initial_delay`, and must not exceed `5m` when retry is enabled.
- Retry only sanitized `*lpkgo.Error` values marked retryable, status-less `REMOTE_UNAVAILABLE`, HTTP `429`, or HTTP `5xx`; never retry cancellation, deadline expiry, invalid configuration, authentication, permission, not-found, local-file, metadata-integrity, or other `4xx` failures.
- Resolve official credentials once before the retry loop; every publication attempt must re-check application existence and reopen/re-stream the LPK.
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

### Task 2: Retry complete official publication attempts safely

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

Use `NewDelay` and `Wait` injections so tests do not sleep and do not depend on random jitter.

- [ ] **Step 3: Run the focused tests and confirm failure**

Run: `go test ./internal/store/official -run 'TestPublisher.*Retry|TestPublisher.*Attempt|TestPublisher.*Cancellation'`

Expected: FAIL because request retry state and the retry loop do not exist.

- [ ] **Step 4: Refactor one publication attempt behind the retry loop**

Keep request validation, changelog/application preparation, token resolution, base URL selection, and client creation before the loop. Move application check/create, upload metadata verification, and review submission into a helper that performs one complete attempt. For every retry, call that helper again so it reopens the LPK through `uploadLPK`.

Create one default delay closure per call:

```go
policy := backoff.New(request.Retry.MaxDelay, request.Retry.InitialDelay)
nextDelay := policy.Duration
```

When `Publisher.NewDelay` is set, use its returned closure instead. When `Publisher.Wait` is nil, use a timer/select helper that returns on `ctx.Done()`.

- [ ] **Step 5: Add exact retry classification and Retry-After handling**

Classify through `errors.As(err, *lpkgo.Error)`, rejecting cancellation/deadline codes first, then accepting `Retryable`, status-less `REMOTE_UNAVAILABLE`, HTTP 429, or 5xx. Mark 429/5xx retryable in `doRequest`; keep metadata-integrity failures non-retryable.

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

### Task 4: Document, teach, verify, and release the feature

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

State that attempts include the initial request; enabled range is 2-10; durations use Go syntax; only connection/TLS/timeout/reset, HTTP 429, and 5xx are retried; every attempt reopens the LPK and repeats the application existence check; credentials are resolved once; cancellation interrupts requests and waits; final errors/results stay compatible; warnings never include credentials or response bodies.

- [ ] **Step 4: Bump the Action bootstrap patch version**

Change `LAZYCAT_ACTION_VERSION` in `action.yml` from `v1.1.14` to `v1.1.15`. Do not change Action inputs, reusable-workflow inputs, or output schemas.

- [ ] **Step 5: Run complete verification**

Run:

```bash
gofmt -w internal/config/types.go internal/config/load.go internal/config/load_test.go internal/store/official/publish.go internal/store/official/publish_test.go internal/publishflow/flow.go internal/publishflow/flow_test.go internal/metadata/skill_test.go
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

