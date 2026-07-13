# Official Store Backoff Retry Design

## Goal

Add opt-in retries for transient LazyCat official-store publishing failures. The existing one-attempt behavior remains the default. Retry delays use `github.com/cloudflare/backoff` with full jitter, and the configuration, README, examples, and repository Skill describe the behavior precisely.

## Public configuration

The retry policy belongs to the official store because the private store has a separate protocol and failure model:

```yaml
stores:
  official:
    retry:
      enabled: false
      max_attempts: 3
      initial_delay: 2s
      max_delay: 30s
```

- `enabled` defaults to `false` so existing repositories retain one attempt and no added delay.
- `max_attempts` counts the initial request and defaults to `3` when omitted. Valid values are 2 through 10 when retry is enabled.
- `initial_delay` defaults to `2s` and must be between `100ms` and `1m`.
- `max_delay` defaults to `30s`, must be at least `initial_delay`, and must not exceed `5m`.
- Duration values use Go duration syntax. Unknown fields continue to fail because configuration decoding is strict.

The fully populated example is documented, while the shipped starter asset includes only `enabled: false` to keep the common configuration small.

## Retry boundary

Retry at the official publication-operation boundary rather than inside a generic HTTP transport. A retry before review submission obtains a fresh upload reader, reopens the LPK file, and repeats the application existence check. This avoids replaying a consumed multipart body and keeps retry behavior specific to official publication.

Credential resolution remains outside the retry loop. Token acquisition failures are not network publication failures and must return immediately.

Creation is only attempted when the application is still absent. Upload failures can restart the operation because reopening and re-uploading the same verified package/version/digest is safe. Review creation is a non-idempotent POST with no pending-review lookup or idempotency key, so an ambiguous status-less network failure or 5xx response from `store.official.review` is returned without replaying upload/review. HTTP 429 remains retryable because it explicitly means the review request was rate-limited. The retry loop never changes package metadata, version, digest, or changelog.

## Retry classification

Retry only errors represented by `*lpkgo.Error` where:

- `Retryable` is true; or
- `Code` is `REMOTE_UNAVAILABLE` and there is no HTTP status, representing connection, TLS, timeout, response-read, or connection-reset failures; or
- the status is HTTP 429 or 5xx.

For `store.official.review`, narrow that rule to HTTP 429 only. Do not replay a review after a status-less network failure or 5xx because the server may already have accepted it.

Do not retry cancellation, deadline expiry, invalid configuration, authentication or permission failures, not-found, local file errors, metadata mismatches, or other 4xx responses. Context cancellation interrupts both active requests and backoff waits immediately.

HTTP 429 and 5xx errors must be marked retryable at their source. If a valid `Retry-After` value is available, the wait is the greater of that value and the Cloudflare jittered delay, capped by `max_delay`. Response bodies and credentials remain excluded from logs and errors.

## Official precheck ordering

After generic LPK verification, an enabled `skip_if_version_exists` lookup runs before official lint precheck. Equality or a newer online SemVer is a no-op and must skip without official credentials, publication, or a redundant lint failure. When publication remains necessary, the official precheck runs before credential resolution. The publisher repeats the precheck for direct callers as defense in depth.

## Backoff and observability

Create one `backoff.Backoff` per publication call with the configured maximum and initial durations. After a retryable failure, call `Duration()` and wait with a context-aware timer.

Emit one structured warning before each retry with only safe fields:

- store (`official`)
- completed attempt number
- maximum attempts
- selected delay
- safe error code
- HTTP status when present

Successful results and final errors preserve the existing output schema. Exhaustion returns the last sanitized publication error, so callers continue to receive `STORE_PUBLISH_FAILED` with the established upstream fields.

## Components

1. `internal/config`: define and validate the retry configuration and duration syntax.
2. `internal/store/official`: execute one publication attempt and wrap it with the optional retry loop.
3. `internal/publishflow`: pass the validated retry policy into the official publisher request.
4. Documentation and repository Skill: update English/Chinese README, configuration references, workflow guidance, starter asset, and Skill metadata tests.

No Action input or reusable-workflow input is added; project YAML remains the single configuration source.

## Testing

- Configuration tests cover defaults, explicit values, malformed durations, invalid ranges, and disabled compatibility.
- Official publisher tests prove default-off makes exactly one attempt.
- Retry tests prove a transient upload connection/5xx failure succeeds on a later attempt, a 4xx fails once, an ambiguous review 5xx is not replayed, exhaustion returns the final error, and cancellation stops a backoff wait promptly.
- Tests inject deterministic delay generation and waiting so they do not sleep or depend on jitter.
- Existing official publish, publish-flow, metadata, full test, race, staticcheck, and repository verification suites must remain green.

## Compatibility and release

This is an additive configuration change. Existing files decode unchanged, existing publication behavior remains one-shot, and result JSON is unchanged. Release as the next patch version, update floating `v1`, then verify a consumer workflow with retry explicitly enabled.
