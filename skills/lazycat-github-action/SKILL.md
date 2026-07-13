---
name: lazycat-github-action
description: Use when setting up, generating, reviewing, or debugging LazyCat GitHub Actions for LPK repositories, including Docker image updates, release/store publishing, historical LPK migration or cleanup, versioned Release assets, Go Template Manifest preservation, static or Exec builds, and ARM64 runners targeting linux/amd64.
---

# LazyCat GitHub Action

Use it to automatically inspect the repository, then create or update `ca-x/lazycat-github-action@v1` configuration and workflows from the application's real package, build, and Manifest files. Keep image targets, build tools, publication policy, and target architecture explicit.

## Choose the supported GitHub interface

Both repository entry points are supported and use the floating `v1` release tag:

| Entry point | Reference | Responsibility |
|---|---|---|
| Composite Action | `ca-x/lazycat-github-action@v1` | Runs one `check`, `build`, or `publish` operation inside a caller-owned job. The caller owns checkout, permissions, toolchains, Release handling, and any other GitHub mutation. |
| Reusable Workflow | `ca-x/lazycat-github-action/.github/workflows/lazycat.yml@v1` | Owns the complete automation path: toolchains, pull requests, Artifacts, tags, Releases, versioned assets, store reconciliation, and private/official publication. |

Default to the reusable workflow for generated project automation. Use the composite Action only when the existing workflow already provides the surrounding lifecycle. Do not describe one entry point as replacing or disabling the other.

## Primary outcome: working GitHub workflows

Unless the user asks for review-only guidance, finish with repository changes rather than prose alone:

1. Inspect the project and existing automation.
2. Create or update `.github/lazycat-action.yml` from [assets/lazycat-action.yml](assets/lazycat-action.yml).
3. Create or update the appropriate `.github/workflows/*.yml` from [assets/lazycat-workflow.yml](assets/lazycat-workflow.yml).
4. Preserve unrelated workflow jobs and repository conventions.
5. Run `actionlint` plus the project's relevant build/test commands.
6. Report the files created, unresolved placeholders, required Secrets, and verification results.

Do not stop after printing sample YAML when repository editing is authorized. Write the files, validate them, and leave the repository in a reviewable state.

Choose the workflow mode before writing either file:

| Intended automation | `update.strategy` | Version source | Workflow trigger |
|---|---|---|---|
| Scheduled image review PR | `pull` | `image` | `schedule` / `workflow_dispatch` |
| Tag-built GitHub Release Asset | `publish` | `git` | `push.tags` |
| Release-triggered store publication | `publish` | `git` or configured `image` | `release.published` |
| Direct image update, tag, and Release | `publish` | `image` | `schedule` / `workflow_dispatch` |

Never use `pull` for a workflow whose required outcome includes a Git tag, GitHub Release, Release Asset, or store submission.

## Inspect before generating

Read these files when present:

1. `package.yml`: package ID, current version, name, description, locales, icon.
2. `lzc-build.yml`: Manifest path, content directory, buildscript, local image construction.
3. The configured Manifest: application image, services, routes, Exec launch commands.
4. Project toolchain files: `go.mod`, `Cargo.toml`, `rust-toolchain.toml`, `package.json`, lockfile, Dockerfile.
5. Existing `.github/lazycat-action.yml` and workflows.
6. `git status --short`, tracked historical LPKs, `.gitignore`, and pre-existing or untracked `.github/` files.

Do not infer a “main service” from route order, service order, or the first image. Ask for or identify the exact image that drives the application version and the exact service/application field each image updates.

## Historical LPK migration

Before generating Release automation, inventory tracked LPKs with `git ls-files '*.lpk'`. Report the file count and total bytes; do not confuse untracked files with Git history. Keep the configured build output outside packaged content.

## 🔴 CHECKPOINT — before deleting tracked LPKs

Run `git ls-files '*.lpk'`, show the tracked paths, count, and total bytes, recommend cleanup, and **STOP for an explicit yes/no answer immediately before deletion**. General authorization such as “handle it directly” is not deletion approval.

- If the user declines, preserve every tracked LPK and report that the migration remains incomplete.
- If the user approves, remove only the inventoried tracked LPKs, verify the post-delete tracked count, and add `*.lpk` plus the generated output directory to `.gitignore`. An ignore rule does not untrack a file by itself.
- Never rewrite Git history or backfill historical GitHub Releases unless the user separately requests that work.

Future version-bearing releases use `versioned-release-asset: true`. The verified build output remains the validation Artifact and Release upload uses the copied `<package-id>-v<version>.lpk`. The private store uses the verified GitHub Release Asset URL and SHA256. The official store uploads the same locally verified LPK bytes and SHA256 without receiving the Release URL.

## Go Template Manifest safety

Before YAML handling, detect standalone Go Template controls: `if`, `else`, `end`, `with`, and `range`, including indentation and trim markers. You must never execute or evaluate repository templates or invent deployment values.

Protect every standalone control line, perform the narrow YAML inspection/edit, and restore each line byte-for-byte in its original order. Leave inline expressions such as `PASSWORD={{.U.password}}` untouched, and fail closed on a reserved-marker collision, invalid protected YAML, lost or duplicated markers, missing or duplicate image targets, or any unexpected control-line/diff change. Verify the ordered control lines before and after, then run the real LazyCat build or validation command. An ordinary YAML parse/serialize round trip over the raw templated Manifest is unsafe.

## 🔴 CHECKPOINT — before writing project files

Confirm all repository-specific decisions that affect generated files:

- exact package, build, and Manifest paths;
- version source and every managed image target;
- `pull` versus `publish` strategy and enabled stores;
- required build toolchains and fixed `linux/amd64` application target;
- Secret names and transport paths, without reading or reproducing Secret values.

Also compare `project.output` with `lzc-build.yml` `contentdir`. Keep the final LPK outside the packaged content tree so a build cannot include its own output.

If any required decision cannot be proven from inspected files or the user's request, **STOP before editing** and ask for that missing fact. If the user explicitly requests a template, use conspicuous placeholders and list every unresolved value; never describe that template as deployment-ready.

## Choose the project path

| Project shape | Version source | Images | Workflow toolchain |
|---|---|---|---|
| Docker service/application image | `image` with explicit image ID | One entry per managed target | `docker` only when buildscript builds locally |
| Static Web | `git` | None | Usually `node` |
| Exec binary | `git` | None unless runtime also uses an image | `go`, `rust`, or `node` |
| Prebuilt content | `git` | None | `none` |

Maintained source-build references:

- Go Exec: [`lazycat-contrib/cat-led`](https://github.com/lazycat-contrib/cat-led) shows a Git version source, explicit Go toolchain, versioned Release Asset, and dual-store publication.
- Rust + Node Exec: [`lazycat-contrib/lazycat-neko-webshell`](https://github.com/lazycat-contrib/lazycat-neko-webshell) shows a Vite frontend embedded into a Rust musl binary, pinned GitHub-downloaded `protoc`, native Runner dependencies, PR LPK validation, and Tag-only dual-store publication.

Use these as contract references, not blind templates. Re-inspect package paths, buildscript dependencies, target triple, store name, locales, and Secret needs in the target repository.

Use `update.strategy: pull` for scheduled review PRs. Use `publish` only when the workflow should commit/tag/release and optionally publish stores.

## Configure Docker images

For every managed image, set:

- stable `id`;
- `target: service` plus exact `service`, or `target: application` with no service;
- upstream `source`;
- channel and filters;
- delivery mode.

Delivery policy:

| Mode | Result | Local Docker | Official store |
|---|---|---:|---:|
| `lazycat` | Remote copy to `registry.lazycat.cloud` | Not required | Allowed |
| `direct` | Manifest uses upstream reference | Not required | Forbidden |
| `mirror` | Manifest uses explicit accelerator template | Not required | Forbidden |

Set `require_digest_match: true` for mirrors when the accelerator must contain exactly the source `linux/amd64` image.

When a tag needs normalization, reference named `version_regex` groups directly in `version_template`, for example `(?P<version>\d{8})\.0*(?P<build>[1-9]\d*)` with `{version}.{build}.0`. Keep the required `version` group. Unknown placeholders and non-SemVer results fail closed; do not add repository-specific rewriting when this mapping is sufficient.

For SemVer sorting, rank filtered tag names before manifest inspection and stop after the first usable `linux/amd64` candidate; continue past a higher tag only when that tag lacks the target platform. Keep full manifest inspection for `created` sorting because creation time is required for ranking.

Keep `update.allow_downgrade: false` or omit it for the safe default. Compare the mapped version-source image SemVer with the current package version before delivery. Equal versions may refresh an image reference or digest. Set `allow_downgrade: true` only after the user explicitly confirms an intentional rollback.

## 🔴 CHECKPOINT — before enabling version downgrades

Show the current package version, selected lower version, affected version-source image, and why the rollback is required. **STOP for an explicit yes/no answer immediately before writing `allow_downgrade: true`.** General authorization to fix CI or update dependencies is not rollback approval. If the user declines, keep the default guard and repair the channel, sorting, or version mapping instead.

Read [references/configuration.md](references/configuration.md) for channel rules, the configuration schema, authentication, and store constraints.

## Configure builds and workflows

The Action may execute on Linux amd64 or Linux arm64. The LazyCat target is always:

```text
LAZYCAT_TARGET_OS=linux
LAZYCAT_TARGET_ARCH=amd64
LAZYCAT_TARGET_PLATFORM=linux/amd64
```

Buildscripts must honor those values. Go uses `GOOS=linux GOARCH=amd64`; Rust uses `x86_64-unknown-linux-gnu`; TypeScript Exec packages a Linux x64 runtime; Docker Buildx uses `--platform linux/amd64`. ARM64 Docker builds that execute x64 `RUN` steps need QEMU.

The reusable workflow's `toolchains` input must match `build.toolchains` in Action configuration. Do not rely on an implicit moving toolchain version.

Set `build.run_buildscript: false` explicitly when `lzc-build.yml` has no `buildscript`; the Action default is `true`. For every publishing workflow, explicitly map each credential required by the enabled stores under the reusable job's `secrets:` block. Do not use only `secrets: inherit`: explicit mappings make missing repository authorization and Environment/Repository/Organization overrides reviewable. A public-image scheduled PR workflow with stores disabled should not receive unrelated repository Secrets.

For versioned Release assets, set the reusable workflow input exactly:

```yaml
with:
  versioned-release-asset: true
```

The final Release Asset name is `<package-id>-v<version>.lpk`. Verify package ID, package version, Release tag, asset filename, and SHA256. Verify the private-store download URL identifies that asset. Official publication reuses the same locally verified LPK bytes and SHA256, not the Release URL.

Copy [assets/lazycat-action.yml](assets/lazycat-action.yml) and [assets/lazycat-workflow.yml](assets/lazycat-workflow.yml) as starting points, then replace only values confirmed from the inspected project. Read [references/workflows.md](references/workflows.md) for tag/release, permissions, secrets, PR, and store examples.

## Configure stores

Official publishing requires:

- `update.strategy: publish`;
- `stores.official.enabled: true`;
- optional `stores.official.skip_if_version_exists: true` to query the anonymous official catalog and skip an equal version (`version-already-online`) or, with `allow_downgrade: false`, a newer online SemVer (`online-version-newer`);
- optional retry policy, defaulting to `retry.enabled: false`; when enabled, `max_attempts` includes the first attempt and `initial_delay`/`max_delay` use Go duration syntax;
- only `lazycat` image delivery;
- official lint compliance, including locales and icon size at most 200 KB;
- `LAZYCAT_TOKEN`, `LZC_CLI_TOKEN`, username/password, or an explicit `token-file`.

Use this complete retry shape when the project explicitly opts in:

```yaml
retry:
  enabled: false
  max_attempts: 3
  initial_delay: 2s
  max_delay: 30s
```

Upload/check failures may retry status-less connection/TLS/reset failures, HTTP 429, and HTTP 5xx. Review creation retries only HTTP 429; do not replay an ambiguous review network/5xx outcome because the non-idempotent request may already have been accepted. Never retry cancellation, deadline expiry, authentication, permissions, NotFound, integrity failures, HTTP 400, or another 4xx. A retry before review rechecks application existence and reopens the LPK; credentials resolve once. A valid `Retry-After` may extend the jittered wait up to `max_delay`.

Keep lint severity store-scoped. A compatibility warning such as unknown `container_name` stays visible without blocking the shared build or private publication. Only official warnings block the official precheck. If official publishing fails in a dual-store reusable workflow, preserve the private result, emit a warning, and set `failureReason: official-publish-failed`; an official-only workflow remains fatal. With the official store disabled, do not run official lint blocking, precheck, credential resolution, or publication.

For an HTTP rejection, keep status and stage (`store.official.upload` or `store.official.review`). Never expose the raw response body. A recognized JSON `message`, `msg`, string `error`, or nested `error.message` may be displayed only after one-line normalization, a 512-byte bound, and credential-marker suppression.

Private publishing requires:

- `update.strategy: publish`;
- `stores.private.enabled: true`;
- optional `stores.private.skip_if_version_exists: true` to query the exact package before reading write credentials;
- `APPSTORE_URL` and `APPSTORE_TOKEN`;
- optional `APP_ID`;
- optional GitHub Secret `PRIVATE_STORE_GROUP_CODES`, comma-separated, for private groups;
- a real GitHub Release Asset URL and the local SHA256.

When `APP_ID` is absent, preserve a confirmed `stores.private.name`: publishing searches by exact package ID first and then calls authenticated `GET /api/v1/apps/by-name?name=...`. The resolver must return the unique exact-name app for which the Token can upload versions. A 404 permits app creation; ambiguity, authorization failure, an inexact name, or `canUploadVersion: false` must STOP. A name-resolved historical app may have a different package ID; use its numeric ID only for `POST /api/v1/apps/{id}/versions`.

Private stores support `lazycat`, `direct`, `mirror`, static Web, and Exec applications. Never enable the official store merely to get stricter lint for a direct/mirror application; that configuration is intentionally invalid.

Both skip options default to false. When enabled, exact equality returns `published: false`, `skipped: true`, and `skipReason: version-already-online`. If both values are valid SemVer, an online version greater than the candidate also skips with `skipReason: online-version-newer` while `update.allow_downgrade: false`; explicit rollback authorization through `allow_downgrade: true` continues publishing. A non-SemVer value disables ordering and keeps exact-equality-only behavior. Apply this independently per store before resolving write credentials. Not-found continues publishing; every other lookup failure stops. `dry-run` never queries stores. Group codes are secrets: do not put them in Action YAML, ordinary inputs, generated outputs, summaries, or examples with real values.

For scheduled `publish` workflows, treat the exact versioned GitHub Release Asset as the delivery source of truth. If `<package-id>-v<version>.lpk` already exists for the current tag and a store lacks that version, download that exact asset beneath `project.root`, require a GitHub `sha256:` digest, recompute local SHA256, and publish the verified bytes. If a store already has the version, skip it independently. If the Release, exact asset name, or digest is missing, do not select another asset or infer another version; continue only through the normal build/Release path.

Organization Secrets must authorize the target repository. For the same Secret name, GitHub applies the most specific scope: Environment overrides Repository, and Repository overrides Organization. Use organization Secrets as shared defaults and repository Secrets only for intentional overrides; state the effective scope when reviewing a workflow with duplicate names.

## Verify the generated result

Before finishing:

1. Confirm every image target exists exactly once in the Manifest.
2. Confirm `update.version_source.image` names a configured image when type is `image`.
3. Confirm official store plus direct/mirror is absent.
4. Confirm source-build scripts output Linux x86_64.
5. Confirm workflow toolchains and configured toolchains match.
6. Confirm permissions include `contents: write` and `pull-requests: write` for the reusable workflow.
7. Confirm secrets are referenced, never embedded.
8. Confirm `PRIVATE_STORE_GROUP_CODES` is a GitHub Secret when private groups are required.
9. Confirm every enabled store credential is explicitly assigned in the caller workflow and the selected Organization Secret authorizes the repository.
10. Confirm standalone Go Template control lines are byte-identical and were never evaluated.
11. Confirm the private store uses the verified versioned Release URL/SHA256 and official publication uploads the same verified bytes/SHA256 without that URL.
12. Confirm an existing exact Release Asset can reconcile either missing store version without rebuilding or republishing the store that is already current.
13. Run `actionlint` and the project's build/test commands.

## Common failures

| Symptom | First repair | Still failing |
|---|---|---|
| Wrong service image updated | Correct the explicit `service`; never infer it | STOP if the target is missing or duplicated |
| Templated YAML does not parse | Protect supported standalone controls | STOP on invalid protected YAML or marker collision |
| Control-line order/hash changed | Restore exact original control lines | STOP without writing if any marker is lost or duplicated |
| Tracked-LPK inventory fails | Re-run `git ls-files '*.lpk'` and byte accounting | STOP; do not delete from an incomplete inventory |
| Post-delete count mismatches | Compare against the approved inventory | STOP and report the remaining tracked files |
| Versioned asset identity differs | Recompute name, URL, and SHA from verified LPK | STOP before either store submission |
| Official publish rejects registry | Use `delivery.mode: lazycat` for every managed runtime image | STOP if any managed runtime remains direct/mirror |
| `store.official.upload` fails | Confirm the exact verified local LPK path, package/version/SHA256, official lint, and multipart file upload | STOP; never replace the file with a Release URL |
| `store.official.review` fails | Treat the LPK upload as completed; inspect the safe HTTP status and bounded JSON `message` plus application/version review eligibility | A 400 is not retryable; in dual-store automation preserve the private result and warn, while official-only remains fatal |
| ARM64 Runner produced ARM app | Honor `LAZYCAT_TARGET_ARCH=amd64` | STOP until the build proves Linux x86_64 output |
| Private publish has no URL | Resolve the verified GitHub Release Asset | STOP before `publish-private` |
| Equal store version is submitted again | Enable `skip_if_version_exists` and inspect `onlineVersion` | STOP on lookup errors other than not-found |
| Official review returns 400 for an older LPK | Compare candidate and `onlineVersion`; `7.8.138 > 7.7.406` must skip with `online-version-newer` | Do not retry or set `allow_downgrade: true` without explicit rollback approval |
| Release exists but a store is behind | Recover only `<package-id>-v<version>.lpk` and verify both GitHub/local SHA256 | STOP if tag, exact asset, or digest is missing |
| Private application is invisible | Add `PRIVATE_STORE_GROUP_CODES` as a GitHub Secret | STOP rather than commit or print group codes |
| Existing private app conflicts after package lookup | Confirm `stores.private.name` exactly matches the store application and that the store exposes `/api/v1/apps/by-name` | STOP on 401/403/409, an inexact response name, or missing upload permission |
| Rust ConnectRPC build reports `failed to spawn protoc ('protoc')` | Install or download a pinned `protoc` compatible with the repository's declared proto syntax/edition; verify its SHA256 and print `protoc --version` | If system `protoc` rejects `edition = "2023"`, keep the source semantics and use a newer pinned compiler; do not rewrite the proto to `proto3` unless the generated API migration is intentionally implemented and tested |
| PR source build passes but Tag build lacks native tools | Put required tool bootstrap in the authorized buildscript or another path shared by the reusable Tag job | STOP if the dependency exists only in a PR-specific setup step |
| `VERSION_DOWNGRADE_BLOCKED` | Correct an accidental `sort: created` rule or stale tag mapping | Set `allow_downgrade: true` only after explicit rollback confirmation |

## Do Not

- Do not delete tracked LPKs based only on broad repository-edit authorization.
- Do not claim `.gitignore` removed files that are already tracked.
- Do not rewrite history or backfill old Releases as part of routine cleanup.
- Do not execute, render, or evaluate a Go Template Manifest with invented values.
- Do not round-trip a raw templated Manifest through an ordinary YAML serializer.
- Do not publish an unversioned final asset when versioned naming was requested.
- Do not rebuild, rename, or substitute a different Release asset during store reconciliation.
- Do not overwrite pre-existing or untracked `.github/` work.
- Do not expose Secret values in configuration, logs, outputs, summaries, or examples.
- Do not print an official response body; only an explicitly sanitized JSON `message` may cross the error boundary.
- Do not enable `allow_downgrade` merely to make a failing scheduled run green.
