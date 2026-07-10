# LazyCat GitHub Action

[简体中文](README.zh-CN.md)

`ca-x/lazycat-github-action` checks Docker image versions, updates explicit LazyCat Manifest targets, builds LPK files, creates update pull requests, and attaches validated LPK files to GitHub Releases.

The Action uses [`github.com/lib-x/lzc-toolkit-go`](https://github.com/lib-x/lzc-toolkit-go) `v0.1.0`. Its compatibility baseline is `@lazycatcloud/lzc-cli` `2.0.8`.

Current scope:

- Milestone 1: static Web and Exec builds, LPK validation, SHA256, amd64 and arm64 Action binaries.
- Milestone 2: stable, beta, nightly, and custom OCI checks; LazyCat, direct, and mirror delivery; pull requests; Artifacts; tags; Releases; Release Assets.
- Milestone 3: LazyCat official developer-platform submission and Miao private-store submission. The related configuration fields are reserved, but store submission operations are not active yet.

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

Milestone 2 image copy uses a developer-platform token. The Action resolves it in this order:

1. `LAZYCAT_TOKEN`
2. `LZC_CLI_TOKEN`

Do not store an account password in GitHub Actions. Log in once from a trusted machine and save the returned token as a GitHub Actions secret.

When lzc-cli 2.0.8 is already logged in locally, it checks `LZC_CLI_TOKEN` first and then the `token` field in `~/.config/lazycat/box-config.json`. `lzc-cli config get token` prints the effective token, so do not run that command in CI logs. A GitHub-hosted Runner cannot read your local login file; add the token as a repository or organization secret.

The underlying toolkit also supports account/password exchange and explicit token stores. See the [lzc-toolkit-go authentication examples](https://github.com/lib-x/lzc-toolkit-go#example-5-log-in-and-submit-an-lpk). The Action itself accepts tokens only in Milestone 2. Account/password login and store submission orchestration arrive in Milestone 3.

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

Direct publish in Milestone 2 means Git commit, tag, and GitHub Release. It does not submit to the LazyCat official store or a private store yet.

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

## Artifact versus Release Asset

- Every non-empty build result is uploaded as a Workflow Artifact for CI inspection.
- Pull-request mode stops after the Artifact and PR.
- Release flows also attach the LPK to a GitHub Release and return `download-url`.
- The future private-store publisher uses the Release Asset URL plus SHA256, so the store can trust the provided digest without downloading the file just to compute it.

## Dry run

```yaml
with:
  operation: check
  config: .github/lazycat-action.yml
  dry-run: true
```

Dry run selects versions and reports planned references without copying images, editing files, running the buildscript, creating a PR, or creating a Release.

See the [design specification](docs/superpowers/specs/2026-07-10-lazycat-github-action-design.md) for the complete target behavior.
