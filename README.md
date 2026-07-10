# LazyCat GitHub Action

[简体中文](README.zh-CN.md)

`ca-x/lazycat-github-action` builds and verifies LazyCat LPK applications in GitHub Actions. It uses [`github.com/lib-x/lzc-toolkit-go`](https://github.com/lib-x/lzc-toolkit-go) `v0.1.0`, whose compatibility baseline is `@lazycatcloud/lzc-cli` `2.0.8`.

This repository is being delivered in milestones. Milestone 1 supports Git/tag-sourced static and Exec applications. Docker image discovery/copy, automatic pull requests, GitHub Releases, and store publishing are added by the next milestones.

## The architecture rule that matters most

The machine running the Action and the application produced for LazyCat are different concerns:

| Concern | Supported value |
|---|---|
| GitHub Runner OS | Linux |
| GitHub Runner CPU | amd64 or arm64 |
| LazyCat target OS | Linux |
| LazyCat target CPU | amd64 (x86_64) |
| OCI target platform | `linux/amd64` |

An ARM64 self-hosted Runner downloads the ARM64 Action binary, but build scripts still receive:

```text
LAZYCAT_TARGET_OS=linux
LAZYCAT_TARGET_ARCH=amd64
LAZYCAT_TARGET_PLATFORM=linux/amd64
```

Therefore Go, Rust, native Node.js modules, embedded runtimes, and Docker images inside an LPK must remain Linux x86_64. The Action logs both values explicitly:

```text
Action host: linux/arm64; LazyCat target: linux/amd64
```

## Concepts

- `package.yml` contains the package ID, application version, display metadata, and locales.
- `lzc-manifest.yml` describes the LazyCat application, routes, optional application image, and services.
- `lzc-build.yml` tells the toolkit where content lives and which project `buildscript` to run.
- An LPK is the package produced from those files and application content.
- Basic lint checks whether an LPK is structurally usable. Official lint additionally applies LazyCat developer-platform preferences such as locales, official image registry references, icon format/size, and SemVer.
- A Workflow Artifact is a CI result. A GitHub Release Asset is a public versioned download. Milestone 1 uploads an Artifact; Release Asset automation arrives in Milestone 2.

## Minimal project configuration

Create `.github/lazycat-action.yml`:

```yaml
version: 1

project:
  root: .
  build_config: lzc-build.yml
  package_file: package.yml
  output: dist/app.lpk

update:
  strategy: pull
  version_source:
    type: git

build:
  run_buildscript: true
```

Unknown fields are rejected. Milestone 1 requires `version_source.type: git`; image-driven versions are implemented in Milestone 2.

## Static Web example

`lzc-manifest.yml` can contain no services:

```yaml
application:
  subdomain: example
  routes:
    - /=file:///lzcapp/pkg/content
```

`lzc-build.yml`:

```yaml
buildscript: ./scripts/build.sh
contentdir: ./dist/content
```

For a TypeScript static site, `scripts/build.sh` can be:

```bash
#!/usr/bin/env bash
set -euo pipefail
npm ci
npm run build
rm -rf dist/content
mkdir -p dist/content
cp -R web-dist/. dist/content/
```

Static HTML/CSS/JavaScript is normally CPU-independent. Any native Node.js addon used at LazyCat runtime still needs a Linux x86_64 build.

## Exec example

An Exec application also needs no services:

```yaml
application:
  subdomain: example
  routes:
    - /=exec://8080,/lzcapp/pkg/content/app
```

A Go buildscript must use the target variables instead of the Runner architecture:

```bash
#!/usr/bin/env bash
set -euo pipefail
mkdir -p dist/content
CGO_ENABLED=0 \
GOOS="${LAZYCAT_TARGET_OS}" \
GOARCH="${LAZYCAT_TARGET_ARCH}" \
go build -trimpath -ldflags='-s -w' -o dist/content/app ./cmd/app
```

On both amd64 and ARM64 Runners this creates a Linux x86_64 executable.

## Git tag workflow

After the Action's `v1` release, callers use it directly without compiling it:

```yaml
name: Build LPK

on:
  push:
    tags:
      - "v*"

permissions:
  contents: read

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: ca-x/lazycat-github-action@v1
        id: lazycat
        with:
          operation: build
          version: ${{ github.ref_name }}

      - uses: actions/upload-artifact@v4
        with:
          name: lpk-${{ steps.lazycat.outputs.version }}
          path: ${{ steps.lazycat.outputs.lpk-path }}
```

The Action removes one leading `v`, updates `package.yml.version`, runs the project buildscript, builds the LPK, opens it again, checks package ID/version, lints it, computes SHA256, and returns outputs.

Use an ARM64 Runner by changing only `runs-on` to an ARM64 label. Do not change the LazyCat target variables.

## Dry run

```yaml
- uses: ca-x/lazycat-github-action@v1
  with:
    operation: build
    version: v1.2.3
    dry-run: true
```

Dry run reads the project and reports whether `package.yml` would change. It does not edit files, run the buildscript, or produce an LPK.

## Outputs

Important outputs are `changed`, `package-id`, `version`, `tag`, `lpk-path`, `sha256`, `result-file`, `runner-arch`, and `target-platform`. `image-results` is an empty JSON array in Milestone 1 and becomes populated by Milestone 2.

The complete secret-free result is written to `.lazycat-action/result.json`. Passwords, platform tokens, store tokens, Authorization headers, and cookies are not written to outputs or summaries.

## Does the Runner need Docker?

Not for Milestone 1 static or Exec builds unless your own `buildscript` invokes Docker. Later, registry inspection and LazyCat's remote registry-to-registry image copy also do not require local Docker. A local Docker build on ARM64 that executes x64 Dockerfile steps needs Buildx/QEMU and must target `linux/amd64`.

## Current Milestone 1 limits

The public inputs for `check`, `publish-official`, and `publish-private` already exist to keep the Action contract additive, but those operations return `PROJECT_UNSUPPORTED` until their milestones land. Milestone 1 does not create pull requests, tags, Releases, Release Assets, or store submissions.

See the [design specification](docs/superpowers/specs/2026-07-10-lazycat-github-action-design.md) for the complete target behavior.
