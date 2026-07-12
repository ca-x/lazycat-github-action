# Go Quality Audit Design

## Goal

Review the current Go implementation with production, concurrency, performance,
and Go 1.25 guidance, then fix only issues supported by static analysis, tests,
or benchmark evidence.

## Scope

- Fix the two current `staticcheck` findings in version normalization and image
  version mapping.
- Audit the three live concurrency surfaces:
  - official-store streaming LPK upload;
  - image-copy progress aggregation;
  - cached LazyCat token resolution.
- Add focused regression tests for cancellation, writer failure, and goroutine
  completion where the current test suite does not prove those properties.
- Add benchmarks only for code paths where a plausible performance concern can
  be measured in isolation.
- Apply Go 1.25 modern syntax to touched code and tests when it improves clarity
  without broad mechanical churn.

## Non-goals

- No architecture rewrite.
- No repository-wide modernization unrelated to a verified issue.
- No speculative `sync.Pool`, lock replacement, or allocation optimization.
- No public configuration or Action interface changes unless a confirmed bug
  cannot be fixed without one.

## Approach

### Correctness and production practices

Trace each finding through its callers and consumers before editing. Preserve
the current command bootstrap and public behavior. Errors must remain staged and
safe for GitHub Action logs.

### Concurrency

Treat goroutine termination and cancellation as explicit invariants. The
official upload producer must always terminate when request creation, HTTP
execution, context cancellation, or multipart writing fails. Progress state
must remain race-free without holding locks while calling external callbacks.
Token resolution must avoid duplicate unsafe mutation while not caching failed
or empty results.

### Performance

Measure before optimizing. Add narrowly scoped benchmarks with `b.Loop()` and
allocation reporting for any path selected for optimization. Keep a change only
when repeated benchmark results show a meaningful improvement without reducing
correctness or readability.

### Modern Go

The module targets Go 1.25. Touched tests should use `t.Context()` where a test
context is sufficient, and new benchmarks should use `b.Loop()`. New concurrent
test helpers may use Go 1.25 APIs, but production code will not adopt newer APIs
merely for novelty.

## Verification

The completed change must pass:

```text
go test ./...
go test -race ./...
go vet ./...
staticcheck ./...
go test -bench=. -benchmem ./...
bash scripts/run-action_test.sh
git diff --check
```

Where a concurrency bug is fixed, its regression test must fail against the
unfixed implementation and pass after the fix. Performance claims require
before-and-after benchmark evidence rather than inspection alone.

## Success criteria

- All confirmed findings are fixed with focused tests.
- No goroutine leak, data race, or cancellation hang is reproducible in the
  audited concurrency surfaces.
- Static analysis and the complete verification suite pass.
- Any performance change has measured evidence; otherwise the code is left
  unchanged and the audit records that no justified optimization was found.
