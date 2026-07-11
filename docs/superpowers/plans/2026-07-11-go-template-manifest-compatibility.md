# Go Template Manifest Compatibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make project inspection and image editing accept common LazyCat Go Template control blocks without evaluating templates or losing the original control lines, then publish patch release `v1.1.1`.

**Architecture:** Add a focused `internal/manifesttemplate` package that replaces standalone template controls with unique YAML comments and restores them after YAML encoding. Project inspection consumes only the protected bytes; Manifest editing carries the protection record through parsing and restores exact source control lines before its existing atomic write.

**Tech Stack:** Go 1.25+, `go.yaml.in/yaml/v3`, `lzc-toolkit-go v0.2.0`, table-driven Go tests, GitHub Actions, GoReleaser 2, actionlint, ShellCheck.

## Global Constraints

- Never execute or evaluate repository-provided Go Templates.
- Protect only standalone `if`, `else`, `end`, `with`, and `range` control actions; leave inline values such as `PASSWORD={{.U.password}}` untouched.
- Restore each protected control line exactly, including indentation and Go Template trim markers.
- Fail closed on reserved-marker collisions, missing markers, duplicated markers, or YAML that remains invalid after protection.
- Preserve existing project-boundary, symlink, scalar-image, and atomic-write checks.
- Plain YAML behavior must remain unchanged.
- Add reusable-workflow input `versioned-release-asset` as an optional boolean defaulting to `false`.
- When enabled, Release and store publication use `<package-id>-v<version>.lpk`; validation Artifacts keep the configured project output path.
- A Skill may delete tracked historical LPKs only after an explicit user confirmation.
- Release immutable `v1.1.1`, verify it, and only then accept the release workflow's annotated floating `v1` update.

---

### Task 1: Protect and restore standalone template controls

**Files:**
- Create: `internal/manifesttemplate/template.go`
- Create: `internal/manifesttemplate/template_test.go`

**Interfaces:**
- Produces: `type Protected struct` containing protected YAML bytes and private marker metadata.
- Produces: `func Protect([]byte) (Protected, error)`.
- Produces: `func (Protected) Bytes() []byte`.
- Produces: `func (Protected) Restore([]byte) ([]byte, error)`.

- [ ] **Step 1: Write the first failing protection test**

Add a test that passes this literal source to `manifesttemplate.Protect`:

```yaml
application:
  subdomain: templated
{{- if .U.multi_instance }}
  multi_instance: true
{{- else }}
  multi_instance: false
{{- end }}
services:
  app:
    image: registry.lazycat.cloud/example/app:old
    environment:
      - PASSWORD={{.U.password}}
```

Assert that `yaml.Unmarshal(protected.Bytes(), &node)` succeeds, no standalone control text remains in the protected bytes, and the inline password expression remains byte-for-byte present.

- [ ] **Step 2: Run the test and verify RED**

Run: `go test ./internal/manifesttemplate`

Expected: FAIL because `internal/manifesttemplate` does not exist.

- [ ] **Step 3: Implement the minimal protection API**

Create `template.go` with:

```go
package manifesttemplate

type control struct {
	marker   string
	original string
}

type Protected struct {
	data     []byte
	controls []control
}

func Protect(input []byte) (Protected, error)
func (protected Protected) Bytes() []byte
func (protected Protected) Restore(encoded []byte) ([]byte, error)
```

Use the reserved prefix `lazycat-action-template-control-`. Reject input that already contains this prefix. Split input by newline, recognize a standalone action only when its trimmed form begins with `{{`, ends with `}}`, and its normalized first word is one of `if`, `else`, `end`, `with`, or `range`. Replace it with an indentation-preserving comment such as `# lazycat-action-template-control-0`.

`Bytes` returns a defensive copy. `Restore` scans encoded YAML lines by trimmed marker comment, requires exactly one occurrence of every marker, replaces that whole line with the exact original source line, and returns a defensive byte slice.

- [ ] **Step 4: Run the focused test and verify GREEN**

Run: `go test ./internal/manifesttemplate`

Expected: PASS.

- [ ] **Step 5: Add restoration and failure tests one at a time**

Add and run tests for:

- exact restoration of `{{- if ... }}`, `{{- else }}`, and `{{- end }}` after YAML decode/encode;
- `with` and `range` recognition;
- plain YAML round-trip with zero controls;
- reserved-prefix rejection;
- missing-marker rejection;
- duplicated-marker rejection;
- non-control inline expressions remaining untouched.

Run after each test: `go test ./internal/manifesttemplate`

Expected: each new test fails before its minimal implementation adjustment and passes afterward.

- [ ] **Step 6: Commit the isolated component**

```bash
git add internal/manifesttemplate
git commit -m "feat: preserve Go template manifest controls"
```

### Task 2: Integrate protection into inspection and image editing

**Files:**
- Modify: `internal/project/project.go`
- Modify: `internal/project/project_test.go`
- Modify: `internal/manifestedit/images.go`
- Modify: `internal/manifestedit/images_test.go`

**Interfaces:**
- Consumes: `manifesttemplate.Protect`, `Protected.Bytes`, and `Protected.Restore` from Task 1.
- Preserves: public `project.Inspect`, `manifestedit.Read`, and `manifestedit.Apply` signatures.

- [ ] **Step 1: Write a failing project-inspection regression test**

Extend `internal/project/project_test.go` with a project whose Manifest contains:

```yaml
application:
  subdomain: templated
{{- if .U.multi_instance }}
  multi_instance: true
{{- end }}
services:
  app:
    image: registry.lazycat.cloud/example/app:old
```

Call `project.Inspect` through its public API and assert `err == nil`, `Kind == project.KindService`, and the existing package ID/version values are returned.

- [ ] **Step 2: Run the project test and verify RED**

Run: `go test ./internal/project -run Template -count=1`

Expected: FAIL with an invalid Manifest parse error.

- [ ] **Step 3: Protect bytes before typed project parsing**

In `project.Inspect`, after `os.ReadFile(manifestFile)`, call `manifesttemplate.Protect(manifestData)`. Parse `protected.Bytes()` with `manifest.Parse`. Wrap protection errors with the existing `inspect manifest %q` context.

- [ ] **Step 4: Run project tests and verify GREEN**

Run: `go test ./internal/project -count=1`

Expected: PASS.

- [ ] **Step 5: Write a failing image-editor regression test**

Add a public-behavior test in `internal/manifestedit/images_test.go` using a Manifest with the exact `nowledge-mem-lzcapp` shape: standalone `{{- if .U.snap_oidc_allowed_domains }}` and `{{- end }}` lines around an environment entry, followed by `mem` and `nowledge-mem-snap` service images.

Assert that:

- `manifestedit.Read` returns both explicit services;
- `manifestedit.Apply` updates only `mem`;
- the new `# upstream: docker.io/nowledgelabs/mem:0.10.23-vulkan` comment and runtime image are present;
- both original template control lines remain exact;
- the `nowledge-mem-snap` image remains unchanged.

- [ ] **Step 6: Run the image-editor test and verify RED**

Run: `go test ./internal/manifestedit -run Template -count=1`

Expected: FAIL while parsing the raw standalone template control line.

- [ ] **Step 7: Carry protection metadata through Manifest editing**

Extend the private `document` type with:

```go
template manifesttemplate.Protected
```

In `load`, protect the file bytes before `yaml.Unmarshal` and store the returned `Protected`. In `Apply`, after the YAML encoder closes successfully, call `document.template.Restore(encoded.Bytes())`; pass only the restored bytes to `atomicReplace`. Return contextual parse/restore errors and never call `atomicReplace` after a restoration failure.

- [ ] **Step 8: Run focused and combined tests**

Run:

```bash
go test ./internal/manifesttemplate ./internal/project ./internal/manifestedit -count=1
go test -race ./internal/manifesttemplate ./internal/project ./internal/manifestedit -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit the integrations**

```bash
git add internal/project internal/manifestedit
git commit -m "fix: support templated LazyCat manifests"
```

### Task 3: Synchronize documentation, Skill, and patch version

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `skills/lazycat-github-action/SKILL.md`
- Modify: `skills/lazycat-github-action/references/configuration.md`
- Modify: `skills/lazycat-github-action/references/workflows.md`
- Modify: `skills/lazycat-github-action/assets/lazycat-workflow.yml`
- Modify: `skills/lazycat-github-action/test-prompts.json`
- Modify: `internal/metadata/skill_test.go`
- Modify: `internal/metadata/action_test.go`
- Modify: `.github/workflows/lazycat.yml`
- Modify: `action.yml`
- Modify: `docs/superpowers/plans/2026-07-11-go-template-manifest-compatibility.md`

**Interfaces:**
- Documents: standalone template controls are preserved and never executed.
- Produces: optional reusable-workflow input `versioned-release-asset`, default `false`.
- Documents: tracked historical LPK detection and user-confirmed cleanup before Release migration.
- Changes composite bootstrap release from `v1.1.0` to `v1.1.1`.

- [ ] **Step 1: Add failing metadata assertions**

Require the repository Skill and references to state that standalone Go Template controls are preserved during image updates and templates are never evaluated by the Action. Require a visible checkpoint that scans tracked `*.lpk`, reports count/size, and obtains explicit confirmation before cleanup. Require workflow metadata to expose `versioned-release-asset` as an optional boolean with default `false`.

- [ ] **Step 2: Run metadata tests and verify RED**

Run: `go test ./internal/metadata -count=1`

Expected: FAIL because the new contract is not documented.

- [ ] **Step 3: Update bilingual docs and Skill guidance**

Add a concise section to both READMEs and the Skill reference explaining:

- supported standalone controls: `if`, `else`, `end`, `with`, `range`;
- inline deployment expressions remain untouched;
- the Action protects/restores controls but never evaluates them;
- template layouts that are still invalid YAML after protection fail closed.
- versioned Release asset opt-in and exact `<package-id>-v<version>.lpk` naming;
- historical tracked LPK detection, count/size reporting, confirmation, cleanup, and `*.lpk` ignore migration.

Update `SKILL.md` inspection and verification guidance so generated workflows are tested against the repository's real templated Manifest. Add a test prompt for a repository containing many historical tracked LPKs; the expected behavior must stop for cleanup confirmation before deletion and then migrate to versioned Release assets only after approval.

- [ ] **Step 4: Add opt-in versioned Release asset preparation**

Declare this workflow-call input:

```yaml
versioned-release-asset:
  description: Copy the verified LPK to <package-id>-v<version>.lpk for GitHub Release and store publication.
  type: boolean
  required: false
  default: false
```

After Release work is classified, add a shell step that validates the Action outputs are non-empty, copies the verified LPK into `${RUNNER_TEMP}/lazycat-release-assets/<package-id>-v<version>.lpk` when enabled, and otherwise returns the original path. Use this step's path consistently for existing-asset inspection, Release upload, download URL resolution, and both store publication steps. Keep validation Artifact upload unchanged.

- [ ] **Step 5: Update the embedded patch version**

Set `LAZYCAT_ACTION_VERSION: v1.1.1` in `action.yml`. Do not change toolkit `v0.2.0` or lzc-cli compatibility `2.0.8`.

- [ ] **Step 6: Run documentation/version checks**

Run:

```bash
go test ./internal/metadata ./internal/version -count=1
rg -n "Go Template|never evaluat|versioned-release-asset|git ls-files|v1\.1\.1" README.md README.zh-CN.md skills/lazycat-github-action .github/workflows/lazycat.yml action.yml
git diff --check
```

Expected: tests pass, both new contracts appear in both READMEs and the Skill, the workflow input defaults to false, all Release/store paths use the prepared asset, `action.yml` embeds `v1.1.1`, and no whitespace errors are reported.

- [ ] **Step 7: Run Skill TDD and Darwin validation**

Run the historical-LPK prompt without the updated Skill and record whether it deletes files or fails to ask. Run it again with the updated Skill and require count/size reporting plus a user checkpoint. Then run the Darwin nine-dimension assessment and independent prompt comparison; retain only a strict score improvement and record the result in the installed Darwin `results.tsv`.

- [ ] **Step 8: Commit documentation and release metadata**

```bash
git add README.md README.zh-CN.md skills/lazycat-github-action internal/metadata .github/workflows/lazycat.yml action.yml
git commit -m "feat: prepare versioned Release assets"
```

### Task 4: Run release gates and publish v1.1.1

**Files:**
- Modify: `docs/superpowers/plans/2026-07-11-go-template-manifest-compatibility.md` for final evidence only.

**Interfaces:**
- Produces: verified immutable `v1.1.1` Release and annotated floating `v1` at the same release commit.

- [ ] **Step 1: Run the complete local verification gate**

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
go mod verify
git diff --check
```

Expected: every available command exits zero. If local ShellCheck remains unavailable, require the exact-head CI ShellCheck job before tagging.

- [ ] **Step 2: Run the real-repository dry-run before release**

Build the candidate binary, then from a clean temporary clone of `lazycat-contrib/nowledge-mem-lzcapp` run it with the generated `.github/lazycat-action.yml` and:

```bash
INPUT_OPERATION=check \
INPUT_CONFIG=.github/lazycat-action.yml \
INPUT_DRY_RUN=true \
/path/to/lazycat-action
```

Expected: it inspects both public Docker images, reports a structured dry-run result, does not modify `package.yml` or `lzc-manifest.yml`, and does not require a LazyCat token.

- [ ] **Step 3: Commit any final plan evidence and push main**

Record local gates and the real-repository dry-run in this plan, commit the record, push `main`, and locate the exact-head CI run.

- [ ] **Step 4: Require exact-head CI**

Require race tests, vet, actionlint, ShellCheck, Linux amd64/arm64 builds, fixture LPK, and release-contract jobs to pass at the pushed commit.

- [ ] **Step 5: Create and push immutable tag**

Create annotated `v1.1.1` at the verified commit and push only that tag. Wait for the Release workflow to complete.

- [ ] **Step 6: Verify Release assets and tags**

Download `checksums.txt`, amd64/arm64 archives, and SBOMs. Run `sha256sum -c checksums.txt`, inspect archive entries, run the amd64 binary `--version`, and require Action `v1.1.1`, toolkit `v0.2.0`, and target `linux/amd64`. Run `gh attestation verify` for both archives. Confirm remote annotated `v1.1.1` and `v1` dereference to the same commit.

### Task 5: Finish the nowledge-mem-lzcapp integration PR

**Files in temporary clone:**
- Create: `.github/lazycat-action.yml`
- Create: `.github/workflows/lazycat.yml`
- Create or modify: `.gitignore`
- Delete: all Git-tracked root `*.lpk` files after the user's approved cleanup

**Interfaces:**
- Consumes: released `ca-x/lazycat-github-action@v1` resolving to `v1.1.1`.
- Produces: scheduled/manual direct publication for both `mem` and `nowledge-mem-snap`, GitHub Release, official store, and Miaomiao private store.

- [ ] **Step 1: Re-run generated workflow validation**

Require:

- `mem` service source `docker.io/nowledgelabs/mem`, stable `*-vulkan` tags, and package-version extraction;
- `nowledge-mem-snap` service source `docker.io/czyt/nowledge-mem-snap`, stable SemVer tags;
- `mem` as `update.version_source.image`;
- `update.strategy: publish`;
- `build.run_buildscript: false`;
- output `dist/nowledge-mem.lpk`;
- LazyCat delivery for both images;
- official publishing and private publishing enabled with both `skip_if_version_exists: true`;
- private name `NowledgeMem` without requiring `APP_ID`;
- reusable workflow input `versioned-release-asset: true`;
- reusable workflow `ca-x/lazycat-github-action/.github/workflows/lazycat.yml@v1`.

- [ ] **Step 2: Clean approved historical LPKs**

Record `git ls-files '*.lpk'`, file count, and total size in the PR description. Remove every tracked root LPK, add `*.lpk` to `.gitignore`, and confirm no LPK remains tracked. Do not delete package/build/Manifest source files.

- [ ] **Step 3: Run final target-repository gates**

Run actionlint, `bash -n scripts/upgrade-mem.sh`, candidate/released Action dry-run, `git diff --check`, and confirm no LPK is tracked or regenerated. Verify the Release preparation path becomes `community.lazycat.app.nowledge-mem-v<version>.lpk`.

- [ ] **Step 4: Commit, push, and create PR**

Commit on `codex/add-lazycat-action`, push the branch, and create a PR to `lazycat-contrib/nowledge-mem-lzcapp:main` describing managed images, direct Release/store behavior, historical LPK cleanup, verification, and required Secret configuration.

- [ ] **Step 5: Report configuration**

Report required `LAZYCAT_TOKEN`, `APPSTORE_URL`, and `APPSTORE_TOKEN`. State that `APP_ID` is unnecessary because exact `packageId` lookup is used, Docker sources are public, and `PRIVATE_STORE_GROUP_CODES` is needed only when the private application is group-restricted. Explain Secret precedence: Environment overrides Repository, Repository overrides Organization, and an Organization Secret must authorize this repository.

## Verification Record

- Pending implementation and release verification.
