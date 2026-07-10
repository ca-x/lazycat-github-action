# LazyCat Action Milestone 3 Stores and Skill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. The repository owner explicitly requires inline execution by the primary agent; do not dispatch subagents.

**Goal:** Add production-ready LazyCat official-platform authentication and publishing, MiaoMiao private-store publishing, complete workflows/examples, and a validated repository Agent Skill so `main` is ready for the first `v1` release.

**Architecture:** Keep all new Go code under focused `internal` packages. `platformauth` resolves official credentials, `lpkcheck` validates an existing artifact, `store/official` and `store/private` adapt the two real remote APIs, and `publishflow` composes those pieces for `internal/action`. Extend Action inputs and outputs additively, then let the reusable workflow perform GitHub Release orchestration before invoking store operations.

**Tech Stack:** Go 1.25, `github.com/lib-x/lzc-toolkit-go v0.1.0`, `net/http`, `go.yaml.in/yaml/v3`, composite GitHub Actions, reusable workflows, GoReleaser v2, Agent Skills format.

## Global Constraints

- Do not use subagents; the primary agent implements, reviews, commits, merges, and pushes every task.
- Preserve compatibility with `@lazycatcloud/lzc-cli 2.0.8` and `lzc-toolkit-go v0.1.0`; do not invent unsupported official-platform protocols.
- The Action may run on Linux `amd64` or `arm64`, while every LazyCat application, image, and compiled artifact target remains `linux/amd64`.
- Keep `pull` as the default update strategy; store publishing is allowed only for `publish` and never for pull-request validation.
- Official publishing is forbidden when any configured image uses `direct` or `mirror`; private publishing supports all delivery modes and projects with no services.
- Private-store external publishing requires both a real GitHub Release Asset HTTPS URL and the locally computed lowercase SHA256.
- Credential precedence is `LAZYCAT_TOKEN`, `LZC_CLI_TOKEN`, username/password login, then an explicitly configured token file.
- Never place tokens, passwords, authorization headers, cookies, signed URLs, or raw remote error bodies in logs, summaries, result JSON, or returned errors.
- All public Action changes are additive: retain existing inputs, outputs, JSON fields, event behavior, and error semantics.
- Use table-driven unit tests and `go test -race`; validate all external configuration and remote JSON at package boundaries.

---

### Task 1: Freeze the store configuration and Action result contracts

**Files:**
- Modify: `internal/config/types.go`
- Modify: `internal/config/load.go`
- Modify: `internal/config/load_test.go`
- Modify: `internal/project/project.go`
- Modify: `internal/project/project_test.go`
- Modify: `internal/action/action.go`
- Modify: `internal/action/action_test.go`

**Interfaces:**
- Produces: `config.OfficialStore.Application`, `config.PrivateStore.Name`, `config.PrivateStore.Summary`.
- Produces: project metadata fields `Name` and `Description` for store creation defaults.
- Produces: additive Action result fields `OfficialStoreEnabled`, `PrivateStoreEnabled`, and `StoreResults`.

- [x] **Step 1: Write failing configuration and project metadata tests**

  Add table cases proving that official locales default to `zh` and `en`, locale values are normalized and unique, official create metadata is rejected when `create_if_missing` is false, and private name/summary fields are trimmed. Extend project fixture assertions to require package `name` and `description` to be returned.

- [x] **Step 2: Run focused tests and confirm the new expectations fail**

  Run: `go test ./internal/config ./internal/project ./internal/action`

  Expected: FAIL because the new configuration, project, and result fields do not exist.

- [x] **Step 3: Add the contract types and boundary validation**

  Implement these additive shapes:

  ```go
  type OfficialApplication struct {
      Language     string `yaml:"language"`
      Name         string `yaml:"name"`
      Source       string `yaml:"source"`
      SourceAuthor string `yaml:"source_author"`
  }

  type OfficialStore struct {
      Enabled         bool                `yaml:"enabled"`
      CreateIfMissing bool                `yaml:"create_if_missing"`
      Locales         []string            `yaml:"changelog_locales"`
      Application     OfficialApplication `yaml:"application"`
  }

  type PrivateStore struct {
      Enabled bool   `yaml:"enabled"`
      Name    string `yaml:"name"`
      Summary string `yaml:"summary"`
  }
  ```

  Default official locales to `[]string{"zh", "en"}`. Add `Name` and `Description` to `project.Info`. Extend `action.Result` with JSON fields `officialStoreEnabled`, `privateStoreEnabled`, and `storeResults`, where an unset result serializes as `{}` rather than `null`.

- [x] **Step 4: Run focused tests and confirm they pass**

  Run: `go test ./internal/config ./internal/project ./internal/action`

  Expected: PASS.

- [x] **Step 5: Commit the additive contracts**

  ```bash
  git add internal/config internal/project internal/action
  git commit -m "feat: define store publishing contracts"
  ```

### Task 2: Resolve official-platform credentials without leaking secrets

**Files:**
- Create: `internal/platformauth/resolver.go`
- Create: `internal/platformauth/resolver_test.go`

**Interfaces:**
- Consumes: `auth.StaticToken`, `auth.Client.Login`, and `auth/tokenfile.Store` from `lzc-toolkit-go`.
- Produces: `platformauth.Resolver.Resolve(context.Context, Request) (Result, error)`.

- [x] **Step 1: Write failing precedence, login, token-file, and redaction tests**

  Cover these cases in a table: `LAZYCAT_TOKEN` wins; `LZC_CLI_TOKEN` is second; complete username/password invokes login once and keeps the token in memory; one missing credential fails; explicit token file is last; missing credentials return unauthenticated; errors never contain token or password values.

- [x] **Step 2: Run the resolver test and confirm it fails**

  Run: `go test ./internal/platformauth`

  Expected: FAIL because the package is absent.

- [x] **Step 3: Implement the resolver contract**

  Use these stable types:

  ```go
  type Source string

  const (
      SourceLazyCatToken Source = "lazycat-token"
      SourceLZCCLIToken  Source = "lzc-cli-token"
      SourceLogin        Source = "login"
      SourceTokenFile    Source = "token-file"
  )

  type Request struct {
      TokenFile string
  }

  type Result struct {
      Provider auth.TokenProvider
      Source   Source
  }

  type Resolver struct {
      LookupEnv func(string) (string, bool)
      Login     func(context.Context, auth.Credentials) (auth.Session, error)
      LoadFile  func(context.Context, string) (string, error)
  }
  ```

  Build the final provider with `auth.StaticToken`; never return the token string in `Result`. Reject token-file paths containing symbolic-link components and require the file to be a regular file not writable by group/other on Unix. Do not automatically read a developer machine path unless `TokenFile` was explicitly provided.

- [x] **Step 4: Run resolver tests with the race detector**

  Run: `go test -race ./internal/platformauth`

  Expected: PASS with no credential values in output.

- [x] **Step 5: Commit credential resolution**

  ```bash
  git add internal/platformauth
  git commit -m "feat: resolve LazyCat publishing credentials"
  ```

### Task 3: Validate existing LPK artifacts before remote publication

**Files:**
- Create: `internal/lpkcheck/check.go`
- Create: `internal/lpkcheck/check_test.go`
- Modify: `internal/build/build.go`
- Modify: `internal/build/build_test.go`

**Interfaces:**
- Produces: `lpkcheck.File(context.Context, Request) (Result, error)`.
- Consumes: project root, expected package ID/version, and an LPK path.
- Returns: package ID, version, SHA256, size, and target platform.

- [x] **Step 1: Write failing real-LPK validation tests**

  Build the existing static fixture, then test success, mismatched package, mismatched version, non-LPK input, path outside project root, symbolic-link input, and cancellation.

- [x] **Step 2: Run focused tests and confirm failure**

  Run: `go test ./internal/lpkcheck ./internal/build`

  Expected: FAIL because `lpkcheck` does not exist.

- [x] **Step 3: Implement a reusable reader-based artifact check**

  Define:

  ```go
  type Request struct {
      ProjectRoot       string
      Path              string
      ExpectedPackageID string
      ExpectedVersion   string
  }

  type Result struct {
      Path           string `json:"path"`
      PackageID      string `json:"packageId"`
      Version        string `json:"version"`
      SHA256         string `json:"sha256"`
      Size           int64  `json:"size"`
      TargetPlatform string `json:"targetPlatform"`
  }
  ```

  Open the LPK with `lpk.OpenFile`, read its effective manifest, compare expected metadata, stream SHA256 from the file, and return `platform.TargetPlatform`. Extract the common hashing helper from `internal/build` so both build and publish verification use the same implementation.

- [x] **Step 4: Run focused tests with race detection**

  Run: `go test -race ./internal/lpkcheck ./internal/build`

  Expected: PASS.

- [x] **Step 5: Commit artifact validation**

  ```bash
  git add internal/lpkcheck internal/build
  git commit -m "feat: validate LPK artifacts before publishing"
  ```

### Task 4: Adapt the official LazyCat developer-platform publisher

**Files:**
- Create: `internal/store/official/publish.go`
- Create: `internal/store/official/publish_test.go`

**Interfaces:**
- Consumes: an `auth.TokenProvider`, verified LPK metadata, locale list, changelog, and official application-creation options.
- Produces: `official.Publisher.Publish(context.Context, Request) (Result, error)`.

- [x] **Step 1: Write failing SDK-adapter tests against `httptest`**

  Reproduce the actual SDK request sequence: application existence check, optional create, LPK upload, and review creation. Assert `X-User-Token`/cookie authentication is present on the mock server but absent from returned results and failures. Assert every configured locale receives the changelog and response package/version/SHA mismatch is rejected.

- [x] **Step 2: Run the official-store tests and confirm failure**

  Run: `go test ./internal/store/official`

  Expected: FAIL because the package is absent.

- [x] **Step 3: Implement the official adapter using only toolkit APIs**

  Define:

  ```go
  type Request struct {
      Provider        auth.TokenProvider
      LPKPath         string
      FileName        string
      PackageID       string
      Version         string
      SHA256          string
      Changelog       string
      Locales         []string
      CreateIfMissing bool
      Application     config.OfficialApplication
      DefaultName     string
  }

  type Result struct {
      Published bool   `json:"published"`
      Created   bool   `json:"created"`
      PackageID string `json:"packageId"`
      Version   string `json:"version"`
      UploadURL string `json:"uploadUrl"`
      SHA256    string `json:"sha256"`
  }
  ```

  Construct `appstore.PublishRequest`, map the single release changelog to every locale, derive application name from package metadata when no override is configured, and call `appstore.Client.Publish`. Treat every remote response as untrusted and compare package, version, and SHA256 before returning success.

- [x] **Step 4: Run official-store tests with race detection**

  Run: `go test -race ./internal/store/official`

  Expected: PASS.

- [x] **Step 5: Commit official publishing**

  ```bash
  git add internal/store/official
  git commit -m "feat: publish LPKs to the LazyCat platform"
  ```

### Task 5: Implement the MiaoMiao private-store JSON client and idempotency checks

**Files:**
- Create: `internal/store/private/client.go`
- Create: `internal/store/private/types.go`
- Create: `internal/store/private/client_test.go`

**Interfaces:**
- Consumes: `APPSTORE_URL`, `APPSTORE_TOKEN`, optional `APP_ID`, verified LPK metadata, package name/summary, changelog, download URL, and SHA256.
- Produces: `private.Client.Publish(context.Context, Request) (Result, error)`.

- [x] **Step 1: Write failing boundary and protocol tests**

  Use `httptest` to verify: Bearer authentication; JSON create-app request; JSON external-version request; optional APP_ID path; lookup by exact package ID when APP_ID is absent; same version+SHA returns an idempotent existing result; package mismatch fails; response bodies are size-limited; malformed response JSON fails; 401/403 map to authentication errors without echoing the response body; URL validation rejects non-HTTPS, userinfo, fragments, non-GitHub hosts, and paths outside `/releases/download/`; HTTP is accepted only for loopback test servers.

- [x] **Step 2: Run private-store tests and confirm failure**

  Run: `go test ./internal/store/private`

  Expected: FAIL because the package is absent.

- [x] **Step 3: Implement the real private-store protocol**

  Use a 30-second default HTTP timeout and a 1 MiB response limit. Send:

  ```go
  type createApplicationRequest struct {
      PackageID   string `json:"packageId"`
      Name        string `json:"name"`
      Summary     string `json:"summary"`
      Version     string `json:"version"`
      Changelog   string `json:"changelog,omitempty"`
      SourceType  string `json:"sourceType"`
      DownloadURL string `json:"downloadUrl"`
      SHA256      string `json:"sha256"`
  }

  type createVersionRequest struct {
      Version     string `json:"version"`
      Changelog   string `json:"changelog,omitempty"`
      SourceType  string `json:"sourceType"`
      DownloadURL string `json:"downloadUrl"`
      SHA256      string `json:"sha256"`
  }
  ```

  Set `sourceType` to `GITHUB`. Do not send multipart fields that the real server does not accept for external versions. Parse numeric or string IDs defensively. Return:

  ```go
  type Result struct {
      Published   bool   `json:"published"`
      Created     bool   `json:"created"`
      Existing    bool   `json:"existing"`
      AppID       string `json:"appId"`
      VersionID   string `json:"versionId"`
      PackageID   string `json:"packageId"`
      Version     string `json:"version"`
      DownloadURL string `json:"downloadUrl"`
      SHA256      string `json:"sha256"`
  }
  ```

- [x] **Step 4: Run private-store tests with race detection**

  Run: `go test -race ./internal/store/private`

  Expected: PASS.

- [x] **Step 5: Commit private-store support**

  ```bash
  git add internal/store/private
  git commit -m "feat: publish LPKs to MiaoMiao stores"
  ```

### Task 6: Compose publish operations and expose stable Action outputs

**Files:**
- Create: `internal/publishflow/flow.go`
- Create: `internal/publishflow/flow_test.go`
- Modify: `internal/action/action.go`
- Modify: `internal/action/action_test.go`
- Modify: `internal/githubio/env.go`
- Modify: `internal/githubio/env_test.go`
- Modify: `action.yml`

**Interfaces:**
- Consumes: Task 2 credential resolver, Task 3 LPK verifier, and Task 4/5 publishers.
- Produces: `publishflow.Flow.Publish(context.Context, Request) (Result, error)` and Action `store-results` output.

- [x] **Step 1: Write failing flow and Action operation tests**

  Test official/private enablement, wrong operation, pull-strategy rejection, disabled store rejection, missing LPK, missing changelog for official, missing private URL, dry-run without remote calls, LPK package/version/SHA verification, credential error mapping, retryable remote error propagation, and JSON output encoding. Confirm existing build/check outputs are unchanged.

- [x] **Step 2: Run focused tests and confirm failure**

  Run: `go test ./internal/publishflow ./internal/action ./internal/githubio`

  Expected: FAIL because store operations are not wired.

- [x] **Step 3: Implement publish composition and stable error mapping**

  Define a discriminated target:

  ```go
  type Target string

  const (
      TargetOfficial Target = "official"
      TargetPrivate  Target = "private"
  )

  type Result struct {
      Official *official.Result `json:"official,omitempty"`
      Private  *private.Result  `json:"private,omitempty"`
  }
  ```

  Add Action error codes `RELEASE_ASSET_MISSING`, `STORE_AUTH_FAILED`, and `STORE_PUBLISH_FAILED`. Propagate `Retryable` from typed toolkit or private-client errors. Add `TokenFile` to `action.Input`, read it from `INPUT_TOKEN_FILE`, and wire the operations in `action.Run` instead of returning the Milestone 3 placeholder error.

- [x] **Step 4: Extend composite inputs and outputs additively**

  Add `token-file` input and these outputs to `action.yml`: `official-store-enabled`, `private-store-enabled`, and `store-results`. Add matching `githubio.WriteOutputs` entries while preserving multiline delimiter-injection protection.

- [x] **Step 5: Run focused tests and metadata checks**

  Run: `go test -race ./internal/publishflow ./internal/action ./internal/githubio ./internal/metadata`

  Expected: PASS.

- [x] **Step 6: Commit publish orchestration**

  ```bash
  git add internal/publishflow internal/action internal/githubio action.yml
  git commit -m "feat: expose store publishing operations"
  ```

### Task 7: Publish stores from the reusable workflow after Release Asset creation

**Files:**
- Modify: `.github/workflows/lazycat.yml`
- Modify: `.github/workflows/ci.yml`
- Modify: `scripts/run-action_test.sh`
- Modify: `internal/metadata/action_test.go`

**Interfaces:**
- Consumes: build outputs, confirmed GitHub Release Asset URL, and GitHub secrets.
- Produces: reusable-workflow outputs `official-store-enabled`, `private-store-enabled`, and `store-results`.

- [x] **Step 1: Add failing workflow contract assertions**

  Extend shell/metadata tests to require optional secrets `LAZYCAT_USERNAME`, `LAZYCAT_PASSWORD`, `APPSTORE_URL`, `APPSTORE_TOKEN`, and `APP_ID`; token-file input; store outputs; conditions that require `update-strategy == 'publish'`; and private publishing only after `asset-url` produced a non-empty URL.

- [x] **Step 2: Run workflow contract tests and confirm failure**

  Run: `go test ./internal/metadata && bash scripts/run-action_test.sh`

  Expected: FAIL because the workflow does not expose store publication.

- [x] **Step 3: Add official and private publication steps**

  Pass all credentials only to their specific publish step. Invoke the composite Action with `operation: publish-official` or `publish-private`, the built `lpk-path`, changelog, and private `download-url`. Never pass store credentials to buildscript execution. Merge the two operation results into one deterministic `store-results` JSON output with `jq -cS` or an equivalent checked script.

- [x] **Step 4: Preserve Docker and non-service behavior**

  Keep toolchain setup before build, retain x64 target declarations on ARM64 runners, and ensure static/Exec projects with `images: []` reach the same Release and store steps. Keep pull-request jobs free of all store calls.

- [x] **Step 5: Run actionlint and workflow tests**

  Run: `go test ./internal/metadata && bash scripts/run-action_test.sh && actionlint .github/workflows/*.yml`

  Expected: PASS.

- [x] **Step 6: Commit workflow publication**

  ```bash
  git add .github/workflows scripts/run-action_test.sh internal/metadata
  git commit -m "feat: publish releases to configured stores"
  ```

### Task 8: Add complete bilingual documentation and copyable examples

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `docs/superpowers/specs/2026-07-10-lazycat-github-action-design.md`
- Create: `examples/docker-stable-lazycat/.github/lazycat-action.yml`
- Create: `examples/docker-mirror/.github/lazycat-action.yml`
- Create: `examples/go-exec/.github/workflows/lazycat.yml`
- Create: `examples/go-exec/scripts/build.sh`
- Create: `examples/rust-exec/.github/workflows/lazycat.yml`
- Create: `examples/rust-exec/scripts/build.sh`
- Create: `examples/typescript-static/.github/workflows/lazycat.yml`
- Create: `examples/typescript-static/scripts/build.sh`
- Create: `examples/typescript-exec/.github/workflows/lazycat.yml`
- Create: `examples/typescript-exec/scripts/build.sh`
- Create: `examples/stores/.github/lazycat-action.yml`
- Create: `examples/stores/.github/workflows/lazycat.yml`

**Interfaces:**
- Documents every stable Action input/output, configuration field, permission, secret, architecture rule, and store result.
- Corrects the private-store design section to use the real JSON external-version protocol.

- [x] **Step 1: Add documentation gate assertions before prose**

  Extend the existing documentation test script to require reciprocal language links and searchable headings/phrases for authentication precedence, local lzc-cli token location, Docker installation rules, image copy results, official lint restrictions, private URL+SHA256, APP_ID behavior, static/Exec projects, ARM64 host versus x64 target, and every example directory.

- [x] **Step 2: Run the documentation gate and confirm failure**

  Run the repository's README/documentation checks from `.github/workflows/ci.yml`.

  Expected: FAIL on missing Milestone 3 sections and examples.

- [x] **Step 3: Write Chinese and English end-to-end documentation**

  Explain that GitHub-hosted runners do not inherit local login state. Document token extraction as `LZC_CLI_TOKEN` first and `~/.config/lazycat/box-config.json` field `token` second; warn that `lzc-cli config get token` prints a secret and must not be logged. Show account/password login as a temporary in-memory CI fallback. Include full YAML for official publishing and both private create-app and add-version paths.

- [x] **Step 4: Add runnable build examples**

  Go uses `GOOS=linux GOARCH=amd64`; Rust installs and builds `x86_64-unknown-linux-gnu`; TypeScript static emits architecture-neutral assets; TypeScript Exec packages a Linux x64 runtime/binary. Docker examples state that remote LazyCat copying does not require local Docker, while Dockerfile/buildscript builds do and ARM64 runners require Buildx/QEMU.

- [x] **Step 5: Run documentation and shell checks**

  Run: `git diff --check && shellcheck examples/*/scripts/*.sh scripts/*.sh`

  Expected: PASS.

- [x] **Step 6: Commit documentation and examples**

  ```bash
  git add README.md README.zh-CN.md docs/superpowers/specs examples
  git commit -m "docs: add store publishing and build examples"
  ```

### Task 9: Create and validate the repository Agent Skill

**Files:**
- Create: `skills/lazycat-github-action/SKILL.md`
- Create: `skills/lazycat-github-action/agents/openai.yaml`
- Create: `skills/lazycat-github-action/references/configuration.md`
- Create: `skills/lazycat-github-action/references/workflows.md`
- Create: `skills/lazycat-github-action/assets/lazycat-action.yml`
- Create: `skills/lazycat-github-action/assets/lazycat-workflow.yml`
- Create: `skills/lazycat-github-action/evals/evals.json`

**Interfaces:**
- Produces: a discoverable skill named `lazycat-github-action` that generates or audits project configuration and workflows.
- Covers: Docker service binding, channels, delivery modes, static/Exec builds, official/private stores, permissions, secrets, and host/target architecture separation.

- [x] **Step 1: Write baseline evals before creating the skill**

  Create at least six eval prompts and expected assertions: multi-service Docker with explicit web service; nightly image; mirror/private-only publication; Go Exec on ARM64 runner targeting x64; TypeScript static release; and rejection of official publication with direct images. Run the repository eval validator before the skill exists and record the expected missing-skill failure.

- [x] **Step 2: Initialize the skill with the standard generator**

  Run:

  ```bash
  python3 /home/czyt/.codex/skills/.system/skill-creator/scripts/init_skill.py lazycat-github-action \
    --path skills \
    --resources references,assets \
    --interface display_name="LazyCat GitHub Action" \
    --interface short_description="Generate and audit LazyCat LPK automation" \
    --interface default_prompt="Use $lazycat-github-action to configure this repository for LazyCat LPK builds and publishing."
  ```

- [x] **Step 3: Replace generated placeholders with concise operational guidance**

  Keep `SKILL.md` below 500 lines, put detailed schemas and full workflow variants in one-level references, and provide copyable configuration/workflow assets. Make the skill require inspection of `package.yml`, `lzc-build.yml`, and the manifest before choosing a project type; never infer a main service; and reject architecture/store combinations that violate the Action contract.

- [x] **Step 4: Validate the skill and eval schema**

  Run:

  ```bash
  python3 /home/czyt/.codex/skills/.system/skill-creator/scripts/quick_validate.py skills/lazycat-github-action
  jq empty skills/lazycat-github-action/evals/evals.json
  rg -n "TODO|TBD|placeholder" skills/lazycat-github-action && exit 1 || true
  ```

  Expected: validation passes, JSON parses, and no placeholder text remains.

- [x] **Step 5: Execute deterministic eval assertions locally**

  Add a small Go or shell validator only if required to check `evals.json`; run every expected structural assertion without invoking remote systems or subagents. Confirm each scenario points to the correct reference/assets and encodes explicit service, delivery, store, and architecture decisions.

- [x] **Step 6: Commit the Agent Skill**

  ```bash
  git add skills
  git commit -m "feat: add LazyCat Action agent skill"
  ```

### Task 10: Validate real project shapes, release readiness, and merge Milestone 3

**Files:**
- Modify: `testdata/image-app/*` only when a regression fixture needs store metadata.
- Modify: `testdata/static-app/*` only when a regression fixture needs store metadata.
- Create: `testdata/exec-app/*`
- Modify: `.goreleaser.yml`
- Modify: `.github/workflows/release.yml`
- Modify: `docs/superpowers/plans/2026-07-10-lazycat-action-milestone-3-stores-and-skill.md`

**Interfaces:**
- Produces: a release-ready `main`, signed/checksummed Linux amd64 and arm64 Action assets, and floating `v1` tag behavior.

- [x] **Step 1: Add fixed real-project compatibility fixtures**

  Use `gh` to inspect selected `lazycat-contrib` repositories, record repository and commit SHA in fixture comments, and copy only minimal package/build/manifest shapes. Cover one multi-service Docker project and one no-service static or Exec project without depending on a live default branch during tests.

- [x] **Step 2: Run the complete local verification gate**

  Run fresh:

  ```bash
  go test -race ./...
  go vet ./...
  bash scripts/run-action_test.sh
  shellcheck scripts/*.sh examples/*/scripts/*.sh
  GOOS=linux GOARCH=amd64 go build ./cmd/lazycat-action
  GOOS=linux GOARCH=arm64 go build ./cmd/lazycat-action
  goreleaser check
  actionlint .github/workflows/*.yml examples/*/.github/workflows/*.yml
  git diff --check
  ```

  Expected: every command exits zero.

- [x] **Step 3: Perform the security and compatibility review**

  Inspect all diffs for secrets and sensitive headers, confirm remote response bodies are not surfaced, confirm every HTTP client has context/timeouts/size limits, confirm URL and file boundaries are validated, and compare Action inputs/outputs against the design spec. Run:

  ```bash
  git grep -nE 'lcst_[A-Za-z0-9]|(LAZYCAT|APPSTORE|LZC_CLI)_(TOKEN|PASSWORD)[[:space:]]*[:=][[:space:]]*[^$<{]' -- ':!docs/**' ':!skills/**' ':!**/*_test.go'
  ```

  Expected: no committed credential values.

- [x] **Step 4: Mark completed implementation items and commit the milestone record**

  Update completed implementation and local-verification checkboxes to `[x]`, leave remote merge/release checks open, add a concise verification record with tool versions and results, then commit:

  ```bash
  git add docs/superpowers/plans/2026-07-10-lazycat-action-milestone-3-stores-and-skill.md
  git commit -m "docs: record Milestone 3 completion"
  ```

- [x] **Step 5: Push the milestone branch, merge to main, and re-run the main gate**

  ```bash
  git push -u origin milestone/3-stores-and-skill
  git switch main
  git merge --ff-only milestone/3-stores-and-skill
  go test -race ./...
  go vet ./...
  git push origin main
  ```

  Expected: `origin/main` contains the complete Milestone 3 history and all gates pass after merge.

- [x] **Step 6: Publish the first stable release and floating major tag**

  Verify `action.yml` embeds `v1.0.0`, create and push annotated tag `v1.0.0`, wait for the release workflow, verify both Linux architecture assets and checksums with `gh release view v1.0.0`, then create/update annotated `v1` at the same commit and push it. Do not move either tag if the release asset verification fails.

## Verification Record

- 2026-07-11 local release gate: `go test -count=1 -race ./...`, `go vet ./...`, bootstrap tests, ShellCheck 0.10.0, Linux amd64/arm64 cross-builds, actionlint 1.7.7, Skill validation/evals, and `govulncheck` all passed.
- GoReleaser 2.17.0 `check` and snapshot release passed. Both archives contain only `lazycat-action`, and `checksums.txt` verifies both artifacts.
- Security review confirmed bounded remote responses, 30-second HTTP defaults, disabled redirects for login and both store clients, token-file and LPK symlink rejection, GitHub Release Asset URL/SHA256 binding, and no committed token-shaped values.
- Local toolchain: Go 1.26.5. CI remains pinned to Go 1.25.x and repeats race, vet, workflow, shell, cross-build, fixture, and release-contract checks.
- Milestone branch `62f2719` was pushed and fast-forwarded into `main`; the final release-workflow identity fix is on `main` at `567efd0`, whose GitHub CI run `29121357207` passed.
- Stable tag `v1.0.0` and annotated floating tag `v1` both resolve to release commit `8f4686c`. Release: <https://github.com/ca-x/lazycat-github-action/releases/tag/v1.0.0>.
- Downloaded amd64/arm64 archives and both SPDX 2.3 SBOMs passed `checksums.txt`; the amd64 binary reports Action `v1.0.0`, toolkit `v0.1.0`, lzc-cli `2.0.8`, and target `linux/amd64`.
- GitHub provenance verification passed for both release archives. The first release run completed build, QEMU smoke, SBOM, and attestation but lacked Git tag identity for its last `v1` step; `v1` was created after asset verification and the workflow identity fix was committed for future releases.

## Plan Self-Review

- Spec coverage: Tasks 1–7 cover official authentication, official/private publishing, static/Exec compatibility, structured results, workflow ordering, and secrets. Tasks 8–9 cover every required bilingual example and Agent Skill. Task 10 covers real fixtures, dual architecture, merge, and release.
- Protocol correction: private existing-app publication uses the real JSON external-version request containing `version`, `changelog`, `sourceType`, `downloadUrl`, and `sha256`; multipart remains reserved for direct file upload and is not used by this Action.
- Type consistency: `project.Info` supplies package metadata to both publishers; `lpkcheck.Result` is the shared verified artifact; `publishflow.Result` is serialized unchanged into `action.Result.StoreResults` and the `store-results` output.
- Security boundaries: environment/token file and login response are credential boundaries; LPK path and Release URL are untrusted inputs; official/private responses are untrusted JSON; all are validated before use.
- Placeholder scan: the plan contains no deferred implementation markers; generated skill placeholders must be removed in Task 9.
