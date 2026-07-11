# LazyCat GitHub Action

[简体中文](README.zh-CN.md)

`ca-x/lazycat-github-action` checks Docker image versions, updates explicit LazyCat Manifest targets, builds LPK files, creates update pull requests, and attaches validated LPK files to GitHub Releases.

The Action uses [`github.com/lib-x/lzc-toolkit-go`](https://github.com/lib-x/lzc-toolkit-go) `v0.3.0`. Its compatibility baseline is `@lazycatcloud/lzc-cli` `2.0.8`.

Current scope:

- Milestone 1: static Web and Exec builds, LPK validation, SHA256, amd64 and arm64 Action binaries.
- Milestone 2: stable, beta, nightly, and custom OCI checks; LazyCat, direct, and mirror delivery; pull requests; Artifacts; tags; Releases; Release Assets.
- Milestone 3: LazyCat official developer-platform submission, MiaoMiao private-store submission, complete source-build examples, and the repository Agent Skill.

## Choose the interface

Use the reusable workflow for normal CI/CD. It installs requested toolchains and handles pull requests, Artifacts, tags, Releases, and Release Assets:

```yaml
jobs:
  lazycat:
    uses: ca-x/lazycat-github-action/.github/workflows/lazycat.yml@v1
    with:
      config: .github/lazycat-action.yml
    secrets: inherit
```

Use the composite Action directly when another workflow already handles GitHub mutations:

```yaml
- uses: ca-x/lazycat-github-action@v1
  id: lazycat
  with:
    operation: build
    version: ${{ github.ref_name }}
```

Callers do not compile this repository. The bootstrap downloads a checksum-verified Action binary for the Runner architecture.

## Using the Skill

Ask an agent naturally, for example: “Inspect this LazyCat repository, create the GitHub workflows for versioned Release publishing to both stores, and preserve the Go Template Manifest.” The repository Skill inspects `package.yml`, `lzc-build.yml`, the configured Manifest, toolchain files, `.gitignore`, tracked `*.lpk` files, and existing `.github/` content. It creates or updates `.github/lazycat-action.yml` and the necessary `.github/workflows/*.yml`, then reports every changed file, verification result, unresolved decision, and required GitHub Secret name without reading Secret values.

The Skill pauses before generated project files when paths, image ownership, strategy, stores, or toolchains cannot be proven. For historical LPK migration it runs `git ls-files '*.lpk'`, reports the tracked count and total bytes, and shows a separate visible STOP immediately before deletion. Declining preserves all files. Approval removes only the inventoried files and adds `*.lpk`/output ignore rules; it never rewrites history or backfills old Releases without a separate request.

For version-bearing releases, set `versioned-release-asset: true`. The verified build output remains the validation Artifact and the GitHub Release uses `<package-id>-v<version>.lpk`. The private store receives that verified Release Asset URL and SHA256. The official store uploads the same locally verified LPK bytes and SHA256, but it does not receive the GitHub Release URL.

Go Template Manifests are never evaluated. Standalone `if`, `else`, `end`, `with`, and `range` control lines are protected and restored exactly, including indentation and trim markers; inline expressions remain untouched. The edit fails closed on marker loss/collision, invalid protected YAML, ambiguous targets, or unexpected template changes, and verifies the control lines plus the real build before completion.

## Runner architecture and LazyCat target

The Action host and the LazyCat application target are separate:

| Concern | Supported value |
|---|---|
| Runner OS | Linux |
| Runner CPU | amd64 or arm64 |
| LazyCat target OS | Linux |
| LazyCat target CPU | amd64, x86_64 |
| OCI inspection and copy platform | `linux/amd64` |

An ARM64 self-hosted Runner uses the ARM64 Action binary. Build scripts still receive:

```text
LAZYCAT_TARGET_OS=linux
LAZYCAT_TARGET_ARCH=amd64
LAZYCAT_TARGET_PLATFORM=linux/amd64
```

The reusable workflow accepts a Linux Runner label:

```yaml
jobs:
  lazycat:
    uses: ca-x/lazycat-github-action/.github/workflows/lazycat.yml@v1
    with:
      runner: self-hosted-linux-arm64
      config: .github/lazycat-action.yml
    secrets: inherit
```

The label above is an example. Configure that label on your self-hosted Runner. Changing the Runner does not change the LPK target.

## Concepts

- `package.yml` holds the package ID, version, display metadata, and locales.
- `lzc-manifest.yml` holds the application routes and optional application or service images.
- `lzc-build.yml` points to the Manifest, content, and optional project `buildscript`.
- `.github/lazycat-action.yml` tells this Action which version source and image targets it owns.
- A Workflow Artifact is a CI result retained by GitHub Actions.
- A Release Asset is a public file attached to a GitHub Release.

The Action applies basic LPK lint by default. Set `stores.official.enabled: true` to apply the official LazyCat lint profile. Official mode also requires every configured runtime image to use `delivery.mode: lazycat`.

## Docker image application quick start

Consider an application with a database service named `db` and a visible Web service named `web`:

```yaml
# lzc-manifest.yml
application:
  subdomain: example
  routes:
    - /=http://web:8080/

services:
  db:
    # upstream: postgres:17
    image: registry.lazycat.cloud/acme/postgres:copy-id
  web:
    # upstream: ghcr.io/acme/example-web:v1.0.0
    image: registry.lazycat.cloud/acme/example-web:old
```

The Action never guesses that `web` is the main service. Configure both decisions explicitly:

- `update.version_source.image: web` means the selected `web` image version updates `package.yml.version`.
- `images[].target: service` and `service: web` mean the Manifest editor may change only `services.web.image`.

`db` is already stored in the LazyCat Registry but is not listed under `images`, so this automation leaves it unchanged.

Create `.github/lazycat-action.yml`:

```yaml
version: 1

project:
  root: .
  build_config: lzc-build.yml
  package_file: package.yml
  output: dist/example.lpk

update:
  strategy: pull
  version_source:
    type: image
    image: web

build:
  run_buildscript: true

images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/example-web
    channel: stable
    delivery:
      mode: lazycat

stores:
  official:
    enabled: true
    create_if_missing: false
    changelog_locales: [zh, en]
  private:
    enabled: false
```

Store a developer-platform token as the `LAZYCAT_TOKEN` GitHub secret. `LZC_CLI_TOKEN` is the fallback name.

Then add a scheduled and manual caller workflow:

```yaml
name: Check LazyCat images

on:
  schedule:
    - cron: "17 3 * * *"
  workflow_dispatch:

permissions:
  contents: write
  pull-requests: write

jobs:
  lazycat:
    uses: ca-x/lazycat-github-action/.github/workflows/lazycat.yml@v1
    with:
      operation: auto
      config: .github/lazycat-action.yml
    secrets: inherit
```

`strategy: pull` is the default. When a newer image exists, the workflow updates only the configured targets, builds and validates the LPK, uploads a Workflow Artifact, and opens or updates `lazycat/update-all`.

Use `image-id` to process one configured image:

```yaml
with:
  operation: check
  image-id: web
  config: .github/lazycat-action.yml
```

With `strategy: pull`, selecting a non-version-source image creates a reviewable Manifest change while keeping the current package version. Direct publish requires `image-id` to select the configured version-source image, because a GitHub Release needs a new application version.

## Channels

| Channel | Selection rule |
|---|---|
| `stable` | Highest valid SemVer without a prerelease part |
| `beta` | Highest valid prerelease SemVer, including alpha, beta, rc, and preview labels |
| `nightly` | Newest regex-matched `linux/amd64` OCI image creation time |
| `custom` | Regex filtering with explicit `semver` or `created` sorting |

Stable example:

```yaml
channel: stable
tag_regex: '^v?\d+\.\d+\.\d+$'
exclude_regex: 'windows|arm64'
```

Beta example:

```yaml
channel: beta
tag_regex: '^v?\d+\.\d+\.\d+-(alpha|beta|rc|preview)\.'
```

Nightly example:

```yaml
channel: nightly
tag_regex: '^nightly(-.*)?$'
```

Nightly versions are deterministic SemVer values derived from the selected image creation time and amd64 digest:

```text
0.0.0-nightly.20260710153020.a1b2c3d4e5f6
```

Custom example:

```yaml
channel: custom
sort: created
tag_regex: '^edge-'
version_regex: '^edge-(?P<version>\d+\.\d+\.\d+)$'
version_template: '{version}'
```

`version_template` may reference every named capture from `version_regex`:

```yaml
version_regex: '^(?P<version>\d{8})\.0*(?P<build>[1-9]\d*)$'
version_template: '{version}.{build}.0' # 20260603.01 -> 20260603.1.0
```

The `version` group remains required. Unknown placeholders and expanded values that are not valid SemVer fail closed.

`tag_regex` and `exclude_regex` run before the Action fetches individual manifests. OCI indexes and Docker manifest lists are reduced to `linux/amd64`; ARM64 metadata cannot win selection.

## Image delivery modes

### LazyCat Registry copy

```yaml
delivery:
  mode: lazycat
```

The Action sends the selected source reference to the LazyCat developer platform with `Platform: "amd64"`. The platform performs a remote Registry-to-Registry copy and returns the final `registry.lazycat.cloud/...` reference. Local Docker is not used for this copy.

This mode requires `LAZYCAT_TOKEN` or `LZC_CLI_TOKEN`. It is the only delivery mode accepted when `stores.official.enabled` is true.

### Explicit mirror

```yaml
delivery:
  mode: mirror
  image_template: ghcr.1ms.run/acme/example-web:{tag}
  require_digest_match: true
```

The Manifest uses the expanded mirror reference. `{tag}`, `{digest}`, and `{source}` are supported. With `require_digest_match: true`, the Action inspects the mirror's `linux/amd64` image and requires its digest to match the source digest before editing the Manifest.

### Direct source image

```yaml
delivery:
  mode: direct
```

The Manifest uses the selected source image directly. The Action performs no copy. Use this for a private store or a deployment that intentionally relies on an external Registry or image accelerator.

`direct` and `mirror` are rejected when official-store mode is enabled. They are intended for non-official distribution.

## Does the Runner need Docker?

| Scenario | Docker requirement |
|---|---|
| Inspect public OCI tags and manifests | No |
| LazyCat remote image copy | No |
| Direct or mirror reference update | No |
| Authenticate the reusable workflow to a private source Registry | Docker CLI is required; GitHub-hosted Linux Runners include it |
| Run your own Docker buildscript | Docker is required |
| Execute x64 Dockerfile `RUN` steps on an ARM64 Runner | Docker Buildx and QEMU are required |

Select the Docker toolchain only when the project buildscript needs it:

```yaml
with:
  toolchains: docker
  enable-qemu: true
```

For private source Registry inspection, add these repository secrets:

```text
REGISTRY=ghcr.io
REGISTRY_USERNAME=<username>
REGISTRY_PASSWORD=<token or password>
```

The reusable workflow runs `docker/login-action`, which writes Docker credentials used by the OCI client. These credentials authenticate Action-side inspection. LazyCat's remote `CopyImage` API has no source Registry credential fields in the lzc-cli 2.0.8 contract, so a private source used with `mode: lazycat` must also be pullable by the developer platform.

## Authentication

LazyCat image copy and official LPK publishing resolve credentials in this order:

1. `LAZYCAT_TOKEN`
2. `LZC_CLI_TOKEN`
3. `LAZYCAT_USERNAME` plus `LAZYCAT_PASSWORD`, exchanged for an in-memory token
4. the explicit `token-file` workflow input on a self-hosted Runner

CI should normally store a token. Username/password is supported as a temporary fallback, but keeping an account password as a long-lived GitHub secret is less desirable than a scoped/revocable token. The login response is kept in memory and is not written to disk.

When lzc-cli 2.0.8 is already logged in locally, it checks `LZC_CLI_TOKEN` first and then the `token` field in `~/.config/lazycat/box-config.json`. `lzc-cli config get token` prints the effective token, so do not run that command in CI logs. A GitHub-hosted Runner cannot read your local login file; add the token as a repository or organization secret.

On a trusted self-hosted Runner, an existing lzc-cli-compatible file can be selected explicitly:

```yaml
with:
  token-file: ~/.config/lazycat/box-config.json
```

The file must be a regular file, must not contain symbolic-link path components, and must not grant any group/other permissions. The Action does not automatically inherit a developer workstation login. See the [lzc-toolkit-go authentication examples](https://github.com/lib-x/lzc-toolkit-go#example-5-log-in-and-submit-an-lpk) for the underlying API.

Project builds execute repository-controlled `buildscript` commands. The Action removes LazyCat tokens, Registry credentials, GitHub tokens, and GitHub output/control file paths from the buildscript environment. Keep write-permission release workflows on trusted branches, tags, schedules, and manual runs; do not expose inherited secrets to untrusted pull-request code.

## Pull request and Release workflows

### Safe default: PR, then publish after merge

Use the scheduled workflow above with `strategy: pull`. Add a second caller for the default branch:

```yaml
name: Publish merged LazyCat update

on:
  push:
    branches: [main]

permissions:
  contents: write
  pull-requests: write

jobs:
  lazycat:
    uses: ca-x/lazycat-github-action/.github/workflows/lazycat.yml@v1
    with:
      operation: auto
      config: .github/lazycat-action.yml
    secrets: inherit
```

After the update PR is merged, the default-branch run rebuilds the LPK. If `v<package version>` has no Release, the workflow creates it and uploads the LPK. Existing same-name assets are reused only when GitHub reports the same SHA256 digest; a different digest fails the run.

### Direct publish

Set:

```yaml
update:
  strategy: publish
```

A successful scheduled or manual image check commits only the managed package and Manifest files with `[skip ci]`, pushes the current branch, creates `v<version>`, and uploads the LPK to a GitHub Release. An existing tag is never moved. If it points to another commit, the workflow fails.

Direct publish creates the Git commit, tag, GitHub Release, and Release Asset. If a store is enabled, the reusable workflow then submits the verified LPK to that store. Store publishing never runs for `strategy: pull`.

## Store publishing

Store publication happens only after the workflow has uploaded or safely reused a GitHub Release Asset and confirmed its GitHub-reported SHA256. Projects with no `services` or `images`, including static Web and Exec applications, use the same store flow.

### LazyCat official developer platform

Enable official lint and publishing:

```yaml
update:
  strategy: publish
  version_source:
    type: git

stores:
  official:
    enabled: true
    skip_if_version_exists: true
    create_if_missing: true
    changelog_locales: [zh, en]
    application:
      language: zh
      name: Example App
      source: https://github.com/acme/example
      source_author: acme
```

`create_if_missing: false` publishes only to an application that already exists. When creation is enabled, `application.name` defaults to `package.yml.name`; `language` defaults to `zh`. Official mode enforces the lzc-cli-compatible preferences, including official locales, an icon no larger than 200 KB, SemVer metadata, and LazyCat Registry runtime images. Any configured `direct` or `mirror` image makes configuration fail before publishing.

`skip_if_version_exists: true` performs an anonymous exact-package lookup after the LPK is verified. If the latest official version string equals the verified LPK version, the Action succeeds with `published: false` and `skipped: true` without resolving a developer token or submitting the LPK. Not-found continues publishing; any other lookup failure stops the operation instead of risking a duplicate submission. The option defaults to `false`, and `dry-run` remains network-free.

The reusable workflow accepts `LAZYCAT_TOKEN`, `LZC_CLI_TOKEN`, or `LAZYCAT_USERNAME` plus `LAZYCAT_PASSWORD` as secrets. Token authentication is recommended.

### MiaoMiao private store

Configure the application metadata without putting credentials in the repository:

```yaml
stores:
  official:
    enabled: false
  private:
    enabled: true
    skip_if_version_exists: true
    name: Example App
    summary: Published from CI
```

Add these GitHub secrets:

```text
APPSTORE_URL=https://store.example.com
APPSTORE_TOKEN=lcst_...
APP_ID=42
PRIVATE_STORE_GROUP_CODES=ABC123,LATE23
```

`APP_ID` and `PRIVATE_STORE_GROUP_CODES` are optional. Group codes are access credentials: store them as a GitHub Secret, comma-separated. They are used only by the anonymous latest-version lookup, sent through the toolkit's default `X-Group-Codes` header, and never written to Action inputs, outputs, summaries, or result JSON. The toolkit removes Cookie jars and rejects redirects so group codes are not forwarded to another origin.

With `skip_if_version_exists: true`, the Action queries the exact package through the public Miaomiao latest-version API before reading `APPSTORE_TOKEN`. An equal version returns a successful skipped result. Not-found continues publishing; other lookup failures stop the operation. If `APP_ID` is absent during a real publish, the write client searches for an exact `packageId`; it reuses that application or creates it when no match exists. If it is present, the client verifies that the application's `packageId` matches the LPK before adding a version.

### Release/store reconciliation

Scheduled `publish` workflows also reconcile an existing versioned GitHub Release with both stores. If the current tag already has an exact `<package-id>-v<version>.lpk` asset but a store does not expose that version, the reusable workflow downloads the asset beneath the project root, verifies its GitHub `sha256:` digest and local SHA256, then submits those same bytes. A store already reporting the version is skipped. A missing Release or missing exact asset does not guess another file or version; normal image/build automation remains responsible for creating it.

### GitHub Secret scope and precedence

The reusable workflow reads ordinary GitHub Actions Secrets by name, regardless of whether they are defined for the organization or the repository. Organization Secrets must grant the current repository access through their repository policy.

When the same Secret name exists at multiple levels, the most specific value wins: an Environment Secret takes precedence over a Repository Secret, and a Repository Secret takes precedence over an Organization Secret. For example, a repository-level `APPSTORE_URL` overrides an organization-level `APPSTORE_URL`. Use organization Secrets for shared defaults and repository Secrets only for intentional per-repository overrides. Do not define the same name at several levels unless that override is deliberate.

The Action sends JSON to `POST /api/v1/apps` for a new application or `POST /api/v1/apps/{APP_ID}/versions` for an external version. Both `downloadUrl` and the confirmed 64-character lowercase `sha256` are required. The reusable workflow passes the SHA verified against GitHub to the publish operation, which recomputes the local LPK and rejects any mismatch. The URL must be a real `https://github.com/<owner>/<repo>/releases/download/...` asset URL. The store can record the supplied checksum without downloading the LPK merely to recompute it. The same version and SHA256 is returned as an idempotent existing result; different content under the same version fails.

The private store supports Docker `lazycat`, `direct`, and `mirror` delivery, plus applications with no Docker images. `direct` and `mirror` applications are intentionally not publishable to the official store.

## Tag and release builds for static, Exec, Go, Rust, and TypeScript projects

Projects without Docker services use Git as the version source:

```yaml
update:
  strategy: pull
  version_source:
    type: git
```

Choose either a tag-triggered workflow or a release-triggered workflow. Enabling both for the same tag causes two builds.

Tag trigger:

```yaml
name: Build tagged LPK

on:
  push:
    tags: ["v*"]

permissions:
  contents: write
  pull-requests: write

jobs:
  lazycat:
    uses: ca-x/lazycat-github-action/.github/workflows/lazycat.yml@v1
    with:
      operation: auto
      config: .github/lazycat-action.yml
      toolchains: go
    secrets: inherit
```

Release trigger:

```yaml
name: Build released LPK

on:
  release:
    types: [published]

permissions:
  contents: write
  pull-requests: write

jobs:
  lazycat:
    uses: ca-x/lazycat-github-action/.github/workflows/lazycat.yml@v1
    with:
      operation: auto
      config: .github/lazycat-action.yml
      changelog: ${{ github.event.release.body }}
      toolchains: node
      node-package-manager: pnpm
    secrets: inherit
```

The Action removes one leading `v`, updates `package.yml.version`, runs the project buildscript, builds and reopens the LPK, lints it, computes SHA256, and uploads it to the matching Release. If the tag/release checkout changed `package.yml`, the workflow synchronizes that file to the default branch after a successful asset upload.

### TypeScript static Web build

`lzc-build.yml`:

```yaml
buildscript: ./scripts/build.sh
contentdir: ./dist/content
```

`scripts/build.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
npm ci
npm run build
rm -rf dist/content
mkdir -p dist/content
cp -R web-dist/. dist/content/
```

Use `toolchains: node` and either pass `node-version` or commit `.node-version`.

If `.github/lazycat-action.yml` also declares `build.toolchains`, its toolchain kinds must match the reusable workflow input. Explicit versions must match when both places provide one.

### Go Exec build

```bash
#!/usr/bin/env bash
set -euo pipefail
mkdir -p dist/content
CGO_ENABLED=0 \
GOOS="${LAZYCAT_TARGET_OS}" \
GOARCH="${LAZYCAT_TARGET_ARCH}" \
go build -trimpath -ldflags='-s -w' -o dist/content/app ./cmd/app
```

Use `toolchains: go` and either pass `go-version` or keep the Go version in `go.mod`.

### Rust Exec build

```bash
#!/usr/bin/env bash
set -euo pipefail
cargo build --release --target x86_64-unknown-linux-gnu
mkdir -p dist/content
cp target/x86_64-unknown-linux-gnu/release/example dist/content/app
```

Use `toolchains: rust`. Pass `rust-toolchain`, or commit a `rust-toolchain.toml` with a `toolchain.channel` value. The reusable workflow installs the `x86_64-unknown-linux-gnu` target.

### Docker buildscript

```bash
#!/usr/bin/env bash
set -euo pipefail
docker buildx build \
  --platform "${LAZYCAT_TARGET_PLATFORM}" \
  --load \
  -t example-build:local .
```

Use `toolchains: docker`. On ARM64, keep `enable-qemu: true` if Dockerfile build stages execute x64 programs.

Complete copyable files are under [`examples/`](examples/):

- [`docker-stable-lazycat`](examples/docker-stable-lazycat/.github/lazycat-action.yml) and [`docker-mirror`](examples/docker-mirror/.github/lazycat-action.yml)
- [`go-exec`](examples/go-exec/.github/workflows/lazycat.yml) and [`rust-exec`](examples/rust-exec/.github/workflows/lazycat.yml)
- [`typescript-static`](examples/typescript-static/.github/workflows/lazycat.yml) and [`typescript-exec`](examples/typescript-exec/.github/workflows/lazycat.yml)
- [official and private stores together](examples/stores/.github/workflows/lazycat.yml)

The TypeScript Exec example expects `@yao-pkg/pkg` in the committed lockfile and emits `node22-linux-x64`. TypeScript static assets are architecture-neutral; Go, Rust, TypeScript Exec, and Docker content are explicitly Linux x86_64 even when the Action runs on ARM64.

## Static and Exec Manifests can have no services

Static Web:

```yaml
application:
  subdomain: example
  routes:
    - /=file:///lzcapp/pkg/content
```

Exec:

```yaml
application:
  subdomain: example
  routes:
    - /=exec://8080,/lzcapp/pkg/content/app
```

These projects do not need an `images` section. Their version comes from the tag or release.

## Outputs

| Output | Meaning |
|---|---|
| `operation` | Resolved `check`, `build`, `publish-official`, or `publish-private` operation |
| `changed` | Managed project files changed |
| `package-id` | LazyCat package ID |
| `package-file` | Absolute `package.yml` path |
| `manifest-file` | Absolute Manifest path |
| `version` | Normalized SemVer without a leading `v` |
| `tag` | Normalized `v<version>` tag |
| `lpk-path` | Absolute built LPK path inside the job |
| `sha256` | Lowercase 64-character LPK SHA256 |
| `download-url` | Verified GitHub Release Asset URL when released |
| `image-results` | JSON array of selected and delivered images |
| `store-results` | JSON object containing official/private publication results |
| `official-store-enabled` | Official store is enabled in configuration |
| `private-store-enabled` | Private store is enabled in configuration |
| `update-strategy` | `pull` or `publish` |
| `channel` | Channel of the version-source image |
| `result-file` | Complete secret-free JSON result path |
| `runner-arch` | `amd64` or `arm64` |
| `target-platform` | Always `linux/amd64` in v1 |

Example `image-results` item:

```json
{
  "id": "web",
  "target": "service",
  "service": "web",
  "platform": "linux/amd64",
  "tag": "v2.0.0",
  "sourceRef": "ghcr.io/acme/example-web:v2.0.0",
  "sourceDigest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "deliveryMode": "lazycat",
  "deliveredRef": "registry.lazycat.cloud/acme/example-web:copy-id",
  "copied": true,
  "copyResult": {
    "sourceImage": "ghcr.io/acme/example-web:v2.0.0",
    "platform": "amd64",
    "lazyCatImage": "registry.lazycat.cloud/acme/example-web:copy-id",
    "finished": true
  }
}
```

`.lazycat-action/result.json` contains the complete secret-free result. Tokens, passwords, cookies, and authorization headers are not written to outputs or summaries.

Example `store-results`:

```json
{
  "official": {
    "published": true,
    "skipped": false,
    "created": false,
    "packageId": "cloud.lazycat.example",
    "version": "1.2.3",
    "onlineVersion": "1.2.2",
    "uploadUrl": "/developer/uploads/example.lpk",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "private": {
    "published": true,
    "skipped": false,
    "created": false,
    "existing": false,
    "appId": "42",
    "versionId": "56",
    "packageId": "cloud.lazycat.example",
    "version": "1.2.3",
    "onlineVersion": "1.2.2",
    "downloadUrl": "https://github.com/acme/example/releases/download/v1.2.3/app.lpk",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }
}
```

When an equal online version is found, the selected store result instead contains `published: false`, `skipped: true`, and matching `version`/`onlineVersion`; no write credentials or submission endpoint are used.

## Artifact versus Release Asset

- Every non-empty build result is uploaded as a Workflow Artifact for CI inspection.
- Pull-request mode stops after the Artifact and PR.
- Release flows also attach the LPK to a GitHub Release and return `download-url`.
- Private-store publishing uses the confirmed Release Asset URL plus local SHA256, so the store can trust the provided digest without downloading the file just to compute it.

## Dry run

```yaml
with:
  operation: check
  config: .github/lazycat-action.yml
  dry-run: true
```

Dry run selects versions and reports planned references without copying images, editing files, running the buildscript, creating a PR, or creating a Release.

See the [design specification](docs/superpowers/specs/2026-07-10-lazycat-github-action-design.md) for the complete target behavior.
