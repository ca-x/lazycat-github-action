# Go Quality Audit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix confirmed Go quality issues and verify the live concurrency and performance surfaces with tests and measurements.

**Architecture:** Keep the current package boundaries and public Action contract. Apply two focused static-analysis fixes, add cancellation coverage around the official upload pipeline, and benchmark version mapping before accepting any performance change.

**Tech Stack:** Go 1.25, standard library concurrency primitives, `go test`, race detector, `go vet`, Staticcheck, Go benchmarks.

## Global Constraints

- Target Go 1.25; do not use Go 1.26+ APIs.
- Do not change public configuration or Action interfaces without a reproduced bug requiring it.
- Do not add speculative pools, lock replacements, caches, or allocation tricks.
- Every concurrency production change requires a regression test that fails before the fix.
- Every performance production change requires before-and-after benchmark evidence.
- Preserve staged, secret-safe errors in GitHub Action output.

---

### Task 1: Remove confirmed static-analysis defects

**Files:**
- Modify: `internal/appversion/semver.go:14-22`
- Modify: `internal/versioning/select.go:185-211`
- Test: `internal/appversion/semver_test.go`
- Test: `internal/versioning/select_test.go`

**Interfaces:**
- Consumes: `appversion.Normalize(raw string) (version string, tag string, err error)` and `versioning.Select(rule Rule, candidates []Candidate) (Selection, error)`.
- Produces: unchanged public behavior with no Staticcheck S1017 or SA4006 findings.

- [ ] **Step 1: Add normalization coverage for leading and repeated `v` prefixes**

Add a table case proving a single leading `v` is accepted and `vv1.2.3` remains rejected:

```go
func TestNormalizeLeadingV(t *testing.T) {
	tests := []struct {
		raw         string
		wantVersion string
		wantTag     string
		wantErr     bool
	}{
		{raw: "v1.2.3", wantVersion: "1.2.3", wantTag: "v1.2.3"},
		{raw: "vv1.2.3", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			version, tag, err := appversion.Normalize(test.raw)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected normalization to fail")
				}
				return
			}
			if err != nil || version != test.wantVersion || tag != test.wantTag {
				t.Fatalf("version=%q tag=%q err=%v", version, tag, err)
			}
		})
	}
}
```

- [ ] **Step 2: Run the focused tests before editing production code**

Run: `go test ./internal/appversion ./internal/versioning`

Expected: PASS, establishing behavior that must remain unchanged.

- [ ] **Step 3: Apply the minimal Staticcheck fixes**

Replace the conditional prefix removal with:

```go
value := strings.TrimPrefix(strings.TrimSpace(raw), "v")
```

In `mapVersion`, remove the assignment that is overwritten before use:

```go
if rule.VersionRegex != nil {
	matches := rule.VersionRegex.FindStringSubmatch(tag)
	if matches == nil {
		return "", fmt.Errorf("tag %q does not match version_regex", tag)
	}
	for index, name := range rule.VersionRegex.SubexpNames() {
		if index == 0 || name == "" {
			continue
		}
		groups[name] = matches[index]
	}
}
```

- [ ] **Step 4: Verify the focused packages and Staticcheck**

Run:

```bash
go test ./internal/appversion ./internal/versioning
staticcheck ./internal/appversion ./internal/versioning
```

Expected: both commands exit 0 with no findings.

- [ ] **Step 5: Commit the static-analysis fixes**

```bash
git add internal/appversion/semver.go internal/appversion/semver_test.go internal/versioning/select.go internal/versioning/select_test.go
git commit -m "fix: clean up version normalization"
```

---

### Task 2: Prove official upload cancellation terminates cleanly

**Files:**
- Modify: `internal/store/official/publish_test.go`
- Modify only if the new test fails: `internal/store/official/publish.go:145-196`

**Interfaces:**
- Consumes: `official.Publisher.Publish(ctx context.Context, request official.Request) (official.Result, error)`.
- Produces: a regression test proving cancellation returns promptly and the multipart producer terminates.

- [ ] **Step 1: Add a cancellation regression test**

Add a server that accepts the upload request but does not consume its body, cancel the request context, and require `Publish` to return before a one-second deadline:

```go
func TestPublisherUploadCancellationReturnsPromptly(t *testing.T) {
	path, digest := publishLargeLPK(t)
	uploadStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			close(uploadStarted)
			<-request.Context().Done()
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(ctx, official.Request{
			Provider: auth.StaticToken("ci-token"), LPKPath: path,
			PackageID: "cloud.lazycat.apps.publish-demo", Version: "1.0.0", SHA256: digest,
		})
		done <- err
	}()
	<-uploadStarted
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(time.Second):
		t.Fatal("official upload did not stop after cancellation")
	}
}
```

Create `publishLargeLPK(t)` by reusing `publishLPKWithManifest` with an additional deterministic multi-megabyte file in the test `fstest.MapFS`, so the producer cannot finish before cancellation.

- [ ] **Step 2: Run the regression test under the race detector**

Run: `go test -race ./internal/store/official -run TestPublisherUploadCancellationReturnsPromptly -count=20`

Expected: PASS. If it fails or hangs, retain the failing output as the reproduced bug before changing production code.

- [ ] **Step 3: Fix production code only if Step 2 reproduces a failure**

If necessary, replace the producer completion send with a guaranteed non-blocking close-owned result and join request/write errors after closing the pipe. Preserve the existing staged errors and ensure every early return closes the pipe and waits for the producer.

- [ ] **Step 4: Re-run official-store tests**

Run:

```bash
go test ./internal/store/official
go test -race ./internal/store/official -count=10
```

Expected: PASS with no hang or race report.

- [ ] **Step 5: Commit the concurrency evidence**

```bash
git add internal/store/official/publish.go internal/store/official/publish_test.go
git commit -m "test: cover official upload cancellation"
```

---

### Task 3: Audit progress and token synchronization without speculative rewrites

**Files:**
- Test: `internal/imageflow/flow_test.go`
- Test: `internal/platformauth/resolver_test.go`
- Modify only after a failing test: `internal/imageflow/flow.go`
- Modify only after a failing test: `internal/platformauth/provider.go`

**Interfaces:**
- Consumes: `imageflow.Flow.Check` progress callbacks and `platformauth.Provider.Token`.
- Produces: race-tested invariants for concurrent progress delivery and single successful token caching.

- [ ] **Step 1: Add concurrent callback coverage**

Use a fake deliverer that invokes `Request.OnProgress` concurrently for distinct layers, then assert `Flow.Check` completes and run the test under `-race`. Do not assert log ordering; assert only completion and correct final image result.

- [ ] **Step 2: Add concurrent token caching coverage**

Use a counting provider and start multiple callers simultaneously. Assert all callers receive the same token and successful underlying resolution occurs once. Add a second case where the first resolution fails and a later call retries successfully, proving errors are not cached.

- [ ] **Step 3: Run both tests repeatedly under the race detector**

Run:

```bash
go test -race ./internal/imageflow -count=20
go test -race ./internal/platformauth -count=20
```

Expected: PASS. Modify production synchronization only if a test reproduces a race, duplicate successful resolution, cached failure, or deadlock.

- [ ] **Step 4: Commit synchronization tests or verified fixes**

```bash
git add internal/imageflow/flow.go internal/imageflow/flow_test.go internal/platformauth/provider.go internal/platformauth/resolver_test.go
git commit -m "test: harden concurrent image and token flows"
```

---

### Task 4: Measure version-selection performance

**Files:**
- Modify: `internal/versioning/select_test.go`
- Modify only with benchmark evidence: `internal/versioning/select.go`

**Interfaces:**
- Consumes: `versioning.Select(rule Rule, candidates []Candidate) (Selection, error)`.
- Produces: allocation-aware benchmarks for SemVer and created-time selection.

- [ ] **Step 1: Add Go 1.25 benchmarks**

Create 1,000 deterministic candidates outside the timed loop, call `b.ReportAllocs()`, and use `b.Loop()`:

```go
func BenchmarkSelectStable1000(b *testing.B) {
	candidates := benchmarkCandidates(1000)
	rule := versioning.Rule{Channel: versioning.ChannelStable, Sort: versioning.SortSemVer}
	b.ReportAllocs()
	for b.Loop() {
		selection, err := versioning.Select(rule, candidates)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkSelection = selection
	}
}
```

Add a corresponding custom-created benchmark and a package-level `benchmarkSelection` sink.

- [ ] **Step 2: Record the baseline**

Run: `go test ./internal/versioning -run '^$' -bench=. -benchmem -count=10 > /tmp/versioning-before.txt`

Expected: ten samples per benchmark with `ns/op`, `B/op`, and `allocs/op`.

- [ ] **Step 3: Inspect CPU and allocation profiles**

Run:

```bash
go test ./internal/versioning -run '^$' -bench=BenchmarkSelectStable1000 -cpuprofile=/tmp/versioning.cpu -memprofile=/tmp/versioning.mem
go tool pprof -top /tmp/versioning.cpu
go tool pprof -top -alloc_space /tmp/versioning.mem
```

Expected: a concrete ranked hotspot list. Do not edit production code if no local change can materially improve a measured hotspot.

- [ ] **Step 4: Implement at most one measured optimization**

Permitted examples are preallocating a result slice when its exact size is known or avoiding a proven redundant transformation. Do not introduce pooling or shared mutable caches.

- [ ] **Step 5: Compare results statistically**

Run:

```bash
go test ./internal/versioning -run '^$' -bench=. -benchmem -count=10 > /tmp/versioning-after.txt
go run golang.org/x/perf/cmd/benchstat@latest /tmp/versioning-before.txt /tmp/versioning-after.txt
```

Expected: keep a production optimization only when the result is statistically meaningful and tests remain green. Otherwise revert the production optimization and retain the benchmarks.

- [ ] **Step 6: Commit benchmark coverage**

```bash
git add internal/versioning/select.go internal/versioning/select_test.go
git commit -m "test: benchmark image version selection"
```

---

### Task 5: Complete repository verification

**Files:**
- Modify only if required by verified failures: focused files identified above.

**Interfaces:**
- Consumes: all changes from Tasks 1-4.
- Produces: a release-ready, evidence-backed Go audit result.

- [ ] **Step 1: Run formatting and diff checks**

Run:

```bash
gofmt -w internal/appversion/semver.go internal/appversion/semver_test.go internal/versioning/select.go internal/versioning/select_test.go internal/store/official/publish.go internal/store/official/publish_test.go internal/imageflow/flow.go internal/imageflow/flow_test.go internal/platformauth/provider.go internal/platformauth/resolver_test.go
git diff --check
```

Expected: no output from `git diff --check`.

- [ ] **Step 2: Run the complete correctness suite**

Run:

```bash
go test ./...
go test -race ./...
go vet ./...
staticcheck ./...
```

Expected: all commands exit 0 with no race report or Staticcheck finding.

- [ ] **Step 3: Run benchmarks and Action integration tests**

Run:

```bash
go test -bench=. -benchmem ./...
bash scripts/run-action_test.sh
```

Expected: benchmarks complete and the Action integration script exits 0.

- [ ] **Step 4: Review final scope and history**

Run:

```bash
git status --short --branch
git diff HEAD~4..HEAD --stat
git log --oneline -6
```

Expected: only the approved Go audit, tests, benchmark, specification, and plan are present.

- [ ] **Step 5: Commit any final mechanical cleanup**

Only if formatting or a verified command required a tracked change:

```bash
git add -A
git commit -m "chore: finish Go quality audit"
```
