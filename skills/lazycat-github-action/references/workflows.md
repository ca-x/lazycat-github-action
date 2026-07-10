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
    secrets: inherit
```

Use `update.version_source.type: git`. The workflow updates `package.yml.version`, builds, uploads the Release Asset, and syncs the version to the default branch.

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
```

Only configure the credential family actually needed. Prefer a LazyCat token over username/password. `APP_ID` is optional.

## Source build mapping

| Source | Config toolchain | Workflow input | Required target |
|---|---|---|---|
| Go Exec | `go` | `toolchains: go` | `GOOS=linux GOARCH=amd64` |
| Rust Exec | `rust` | `toolchains: rust` | `x86_64-unknown-linux-gnu` |
| TypeScript static | `node` | `toolchains: node` | architecture-neutral files |
| TypeScript Exec | `node` | `toolchains: node` | Linux x64 packaged runtime |
| Docker buildscript | `docker` | `toolchains: docker` | Buildx `linux/amd64` |

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
  with:
    operation: publish-private
    config: .github/lazycat-action.yml
    version: 1.2.3
    lpk-path: dist/application.lpk
    download-url: https://github.com/acme/example/releases/download/v1.2.3/application.lpk
    changelog: Release notes
```

The LPK path must remain under the project root. The Action reopens the LPK, checks package/version, and computes SHA256 before sending store metadata.
