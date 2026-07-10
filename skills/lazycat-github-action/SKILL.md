---
name: lazycat-github-action
description: Use when configuring, generating, reviewing, or debugging GitHub Actions for LazyCat LPK projects, including Docker image updates, stable/beta/nightly channels, explicit service binding, LazyCat/direct/mirror delivery, static Web or Exec builds, official developer-platform publishing, MiaoMiao private-store publishing, Release Assets, and Linux ARM64 runners that must still produce linux/amd64 applications.
---

# LazyCat GitHub Action

Configure `ca-x/lazycat-github-action@v1` from the application's real package, build, and Manifest files. Keep image targets, build tools, publication policy, and target architecture explicit.

## Inspect before generating

Read these files when present:

1. `package.yml`: package ID, current version, name, description, locales, icon.
2. `lzc-build.yml`: Manifest path, content directory, buildscript, local image construction.
3. The configured Manifest: application image, services, routes, Exec launch commands.
4. Project toolchain files: `go.mod`, `Cargo.toml`, `rust-toolchain.toml`, `package.json`, lockfile, Dockerfile.
5. Existing `.github/lazycat-action.yml` and workflows.

Do not infer a “main service” from route order, service order, or the first image. Ask for or identify the exact image that drives the application version and the exact service/application field each image updates.

## Choose the project path

| Project shape | Version source | Images | Workflow toolchain |
|---|---|---|---|
| Docker service/application image | `image` with explicit image ID | One entry per managed target | `docker` only when buildscript builds locally |
| Static Web | `git` | None | Usually `node` |
| Exec binary | `git` | None unless runtime also uses an image | `go`, `rust`, or `node` |
| Prebuilt content | `git` | None | `none` |

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

Copy [assets/lazycat-action.yml](assets/lazycat-action.yml) and [assets/lazycat-workflow.yml](assets/lazycat-workflow.yml) as starting points, then replace only values confirmed from the inspected project. Read [references/workflows.md](references/workflows.md) for tag/release, permissions, secrets, PR, and store examples.

## Configure stores

Official publishing requires:

- `update.strategy: publish`;
- `stores.official.enabled: true`;
- only `lazycat` image delivery;
- official lint compliance, including locales and icon size at most 200 KB;
- `LAZYCAT_TOKEN`, `LZC_CLI_TOKEN`, username/password, or an explicit `token-file`.

Private publishing requires:

- `update.strategy: publish`;
- `stores.private.enabled: true`;
- `APPSTORE_URL` and `APPSTORE_TOKEN`;
- optional `APP_ID`;
- a real GitHub Release Asset URL and the local SHA256.

Private stores support `lazycat`, `direct`, `mirror`, static Web, and Exec applications. Never enable the official store merely to get stricter lint for a direct/mirror application; that configuration is intentionally invalid.

## Verify the generated result

Before finishing:

1. Confirm every image target exists exactly once in the Manifest.
2. Confirm `update.version_source.image` names a configured image when type is `image`.
3. Confirm official store plus direct/mirror is absent.
4. Confirm source-build scripts output Linux x86_64.
5. Confirm workflow toolchains and configured toolchains match.
6. Confirm permissions include `contents: write` and `pull-requests: write` for the reusable workflow.
7. Confirm secrets are referenced, never embedded.
8. Run `actionlint` and the project's build/test commands.

## Common failures

| Symptom | Correction |
|---|---|
| Wrong service image updated | Add or correct explicit `service`; never infer it |
| Official publish rejects registry | Use `delivery.mode: lazycat` for every managed runtime image |
| ARM64 Runner produced ARM app | Fix buildscript to consume `LAZYCAT_TARGET_ARCH=amd64` |
| Private publish has no URL | Upload/resolve the GitHub Release Asset before `publish-private` |
| GitHub-hosted Runner cannot use local login | Store token as a GitHub secret; local files are not inherited |
| Docker unexpectedly required | Remove `docker` toolchain unless buildscript actually invokes Docker |
