# Workflow reference

## Scheduled pull request

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

Keep `update.strategy: pull`. This uploads a validation Artifact and creates/updates a PR. It does not publish stores.

## Tag source build

```yaml
name: Publish tagged LPK
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
      go-version: 1.25.x
      versioned-release-asset: true
    secrets: inherit
```

Use `update.version_source.type: git`. The workflow updates `package.yml.version`, builds, uploads the Release Asset, and syncs the version to the default branch. With `versioned-release-asset: true`, the Release filename is `<package-id>-v<version>.lpk`. The private store uses its verified Release Asset URL and SHA256; the official store uploads the same locally verified LPK bytes and SHA256 without receiving that URL.

## Historical LPK migration checkpoint

Before writing Release automation, run `git ls-files '*.lpk'`, then report the tracked file count and total bytes. Stop for an explicit yes/no confirmation immediately before deleting those files, even if broad repository editing was already authorized. A decline preserves every tracked LPK. Approval removes only the reported files, verifies the result, and adds `*.lpk` plus the generated output directory to `.gitignore`; ignore rules alone do not untrack files. Never rewrite history or backfill older Releases without a separate request.

## Go Template Manifest workflow safety

Detect standalone `if`/`else`/`end`/`with`/`range` controls before parsing; you must never evaluate a repository Go Template Manifest. Protect and restore exact control lines, leave inline expressions unchanged, and fail closed on collisions, invalid protected YAML, marker loss/duplication, ambiguous targets, or an unexpected template diff. Compare control lines before/after and run the actual LazyCat build or validation command.

## Release-triggered private/official publication

```yaml
name: Publish LPK
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
    secrets: inherit
```

Use `update.strategy: publish`. Configure repository/organization secrets:

```text
LAZYCAT_TOKEN
LZC_CLI_TOKEN
LAZYCAT_USERNAME
LAZYCAT_PASSWORD
APPSTORE_URL
APPSTORE_TOKEN
APP_ID
PRIVATE_STORE_GROUP_CODES
```

Only configure the credential family actually needed. Prefer a LazyCat token over username/password. `APP_ID` is optional. `PRIVATE_STORE_GROUP_CODES` is an optional comma-separated GitHub Secret; never expose it as a normal workflow input.

Organization and repository Secrets use the same names in the reusable workflow. The organization Secret must authorize the repository. For duplicate names, GitHub uses the most specific scope: Environment overrides Repository, and Repository overrides Organization. Treat organization Secrets as shared defaults and repository Secrets as deliberate overrides.

## Existing Release/store reconciliation

A scheduled workflow with `update.strategy: publish`, `versioned-release-asset: true`, and store `skip_if_version_exists: true` is also a repair loop. When the current tag already contains exact asset `<package-id>-v<version>.lpk`, the reusable workflow may download it and fill a missing official or private-store version without rebuilding. It must require the GitHub `sha256:` asset digest, recompute SHA256 after download, keep the file beneath the project root, and let each store independently publish or skip. Missing Release/tag, a different filename, or a missing/mismatched digest is not recoverable by guessing; stop that publication path.

## Source build mapping

| Source | Config toolchain | Workflow input | Required target |
|---|---|---|---|
| Go Exec | `go` | `toolchains: go` | `GOOS=linux GOARCH=amd64` |
| Rust Exec | `rust` | `toolchains: rust` | `x86_64-unknown-linux-gnu` |
| TypeScript static | `node` | `toolchains: node` | architecture-neutral files |
| TypeScript Exec | `node` | `toolchains: node` | Linux x64 packaged runtime |
| Docker buildscript | `docker` | `toolchains: docker` | Buildx `linux/amd64` |

For a real Go source reference, inspect `lazycat-contrib/cat-led`. For a Rust + Node musl reference, inspect `lazycat-contrib/lazycat-neko-webshell`. The latter pins `protoc` from the official GitHub Release with SHA256 verification because Ubuntu's packaged compiler may not support Protobuf Edition 2023. Native dependencies required by Tag publication must be available from the shared buildscript path, not only from a pull-request setup step.

An ARM64 Runner changes only the Action host binary. Preserve the required x64 targets above. Use `enable-qemu: true` for cross-architecture Dockerfile execution.

## Direct composite operations

Use the composite Action directly only when another workflow owns GitHub Releases:

```yaml
- uses: ca-x/lazycat-github-action@v1
  id: publish-private
  env:
    APPSTORE_URL: ${{ secrets.APPSTORE_URL }}
    APPSTORE_TOKEN: ${{ secrets.APPSTORE_TOKEN }}
    APP_ID: ${{ secrets.APP_ID }}
    PRIVATE_STORE_GROUP_CODES: ${{ secrets.PRIVATE_STORE_GROUP_CODES }}
  with:
    operation: publish-private
    config: .github/lazycat-action.yml
    version: 1.2.3
    lpk-path: dist/application.lpk
    download-url: https://github.com/acme/example/releases/download/v1.2.3/application.lpk
    sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    changelog: Release notes
```

The LPK path must remain under the project root. The Action reopens the LPK, checks package/version, and computes SHA256 before sending store metadata.

When `skip_if_version_exists` is enabled, `store-results` includes `skipped` and optional `onlineVersion`. Equal versions skip before write credentials are used; not-found publishes; other lookup failures stop. `dry-run` remains network-free.
