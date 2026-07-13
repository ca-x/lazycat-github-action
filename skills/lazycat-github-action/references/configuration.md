# Configuration reference

## Core shape

```yaml
version: 1
project:
  root: .
  build_config: lzc-build.yml
  package_file: package.yml
  output: dist/application.lpk
  target_arch: amd64
update:
  strategy: pull
  allow_downgrade: false
  version_source:
    type: image
    image: web
build:
  toolchains:
    - kind: go
      version: 1.25.x
  run_buildscript: true
images: []
stores:
  official:
    enabled: false
    skip_if_version_exists: false
    retry:
      enabled: false
  private:
    enabled: false
    skip_if_version_exists: false
```

Unknown fields fail validation. Paths must remain under `project.root`. Output must end in `.lpk`. `project.target_arch` defaults to `amd64` and accepts `amd64` or `arm64`; the target OS remains Linux.

`project.output` is the verified build output and validation Artifact. When the caller sets reusable-workflow input `versioned-release-asset: true`, the workflow copies that verified file to `<package-id>-v<version>.lpk` for the GitHub Release. The copy stays beside the verified LPK under `project.root`. Private publication uses the verified Release Asset URL and SHA256; official publication uploads the same locally verified LPK bytes and SHA256 without receiving that URL.

## Go Template Manifest handling

Manifests may contain standalone Go Template controls `if`, `else`, `end`, `with`, and `range`. Detect them before YAML parsing, never evaluate repository templates, protect each control line during inspection/editing, and restore its exact bytes, indentation, order, and trim markers. Inline expressions remain unchanged. Fail closed on marker collisions, missing/duplicate markers, invalid protected YAML, ambiguous image targets, or changed control lines; do not replace template values with guessed deployment data.

## Version source

- `type: image`: Docker automation; `image` must name one configured image ID.
- `type: git`: tag/release/static/Exec/source builds; `image` must be empty.

The version source answers “which upstream version changes package.yml.” The image target answers “which Manifest field changes.” They are separate decisions.

## Channels

| Channel | Required behavior |
|---|---|
| `stable` | Highest non-prerelease SemVer by default; sort may be `semver` or Docker Hub `updated` |
| `beta` | Highest alpha/beta/rc/preview SemVer by default; sort may be `semver` or Docker Hub `updated` |
| `nightly` | `tag_regex` required; newest target-image creation time; sort is `created` |
| `custom` | `tag_regex` and explicit `semver`, `created`, or Docker Hub `updated` sort required |

Use `exclude_regex` to remove Windows/ARM tags. `version_regex` must contain `(?P<version>...)`; `version_template` defaults to `{version}`. Every named capture is available as an exact placeholder. For example, `^(?P<version>\d{8})\.0*(?P<build>[1-9]\d*)$` plus `{version}.{build}.0` maps `20260603.01` to `20260603.1.0`. Unknown placeholders and non-SemVer expanded values fail closed.

Nightly mutable tags become deterministic SemVer values based on creation time and the configured target-platform digest.

Registry discovery uses `github.com/google/go-containerregistry`. SemVer rules rank filtered tag names before manifest inspection and stop at the first usable configured target, falling back past platform-incompatible higher tags. `sort: updated` uses Docker Hub `last_updated`, then mapped SemVer and tag name; it is Docker Hub-only and fails closed instead of falling back to OCI creation time. `created` rules inspect all eligible manifests because the configured target image creation timestamps determine the result.

## Image target examples

Service:

```yaml
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
    channel: stable
    delivery:
      mode: lazycat
```

Application image:

```yaml
images:
  - id: runtime
    target: application
    source: ghcr.io/acme/runtime
    channel: beta
    delivery:
      mode: direct
```

Mirror:

```yaml
delivery:
  mode: mirror
  image_template: ghcr.1ms.run/acme/web:{tag}
  require_digest_match: true
```

## Build environment

Buildscripts receive version, tag, channel, source date, and LazyCat target variables derived from `project.target_arch`. They do not receive LazyCat credentials, private-store credentials, Registry credentials, GitHub tokens, or GitHub control-file paths.

Only local Docker/buildscript work requires Docker. OCI inspection, direct/mirror edits, and LazyCat remote Registry copying do not invoke local Docker.

## Version downgrade guard

```yaml
update:
  strategy: publish
  allow_downgrade: false
  version_source:
    type: image
    image: web
```

`allow_downgrade` defaults to false. The mapped version-source image SemVer must be greater than or equal to the current package version before delivery or file writes. Equal versions may refresh an image reference or digest. Set true only after the user explicitly confirms an intentional rollback; otherwise `VERSION_DOWNGRADE_BLOCKED` is the required fail-closed result.

## Official store

```yaml
stores:
  official:
    enabled: true
    skip_if_version_exists: true
    create_if_missing: true
    changelog_locales: [zh, en]
    retry:
      enabled: false
      max_attempts: 3
      initial_delay: 2s
      max_delay: 30s
    application:
      language: zh
      name: Example App
      source: https://github.com/acme/example
      source_author: acme
```

Defaults: locales `zh,en`; language `zh`; application name from `package.yml.name`. Application metadata is valid only with `create_if_missing: true`.

`skip_if_version_exists` defaults to false. When true, the Action anonymously queries the exact package after LPK verification. Equality skips with `skipReason: version-already-online`. When both values are valid SemVer, a newer online version skips with `skipReason: online-version-newer` while `allow_downgrade: false`; explicit `allow_downgrade: true` permits publishing. A non-SemVer value uses exact equality only. All skips happen before resolving official credentials. Not-found continues; other lookup errors fail closed. `dry-run` does not query.

`retry.enabled` defaults to false. When enabled, `max_attempts` is 2-10 and includes the first attempt; `initial_delay` and `max_delay` use Go duration syntax. Upload/check failures may retry status-less connection/TLS/reset failures, HTTP 429, and HTTP 5xx. Review creation retries only HTTP 429; a review network failure or 5xx is returned without replay because the request may already have succeeded. Do not retry cancellation, deadline expiry, authentication/permission errors, NotFound, integrity failures, HTTP 400, or another 4xx. A retry before review rechecks application existence and reopens the LPK, while credentials resolve once. Valid `Retry-After` values can extend the jittered delay up to `max_delay`.

Official lint does not turn every compatibility warning into a failure. Unknown `container_name` remains a visible warning; only official warnings block the official precheck, and an equal/newer online version skips before that precheck. Official HTTP failures keep the safe stage and status. The raw body is hidden, while a recognized JSON `message`, `msg`, string `error`, or nested `error.message`/`error.msg` may be displayed after one-line normalization, a 512-byte limit, and credential suppression.

Authentication precedence:

1. `LAZYCAT_TOKEN`
2. `LZC_CLI_TOKEN`
3. `LAZYCAT_USERNAME` plus `LAZYCAT_PASSWORD`
4. explicit `token-file`

Local lzc-cli uses `LZC_CLI_TOKEN`, then `~/.config/lazycat/box-config.json` field `token`. GitHub-hosted runners do not inherit this file.

## MiaoMiao private store

```yaml
stores:
  private:
    enabled: true
    skip_if_version_exists: true
    name: Example App
    summary: Published from CI
```

Secrets: `APPSTORE_URL`, `APPSTORE_TOKEN`, optional `APP_ID`, and optional comma-separated `PRIVATE_STORE_GROUP_CODES`. Group codes never belong in this YAML. They are used only for anonymous exact-package lookup and the toolkit sends them through `X-Group-Codes` with Cookie and redirect isolation.

`skip_if_version_exists` has the same default-off, `version-already-online`, `online-version-newer`, non-SemVer fallback, not-found, fail-closed, and network-free dry-run behavior as the official option. `allow_downgrade: false` protects each store independently. Without `APP_ID`, the write client searches by exact package ID and then resolves `stores.private.name` through authenticated `GET /api/v1/apps/by-name?name=...`. A 404 creates an application with `POST /api/v1/apps`; a unique exact-name writable result supplies the ID for JSON `POST /api/v1/apps/{id}/versions`; every other resolver error stops. Historical package-ID differences are allowed only on this server-authorized name path. Requests always send `sourceType: GITHUB`, the GitHub Release Asset `downloadUrl`, and locally computed `sha256`.

With scheduled `publish` automation, an existing exact `<package-id>-v<version>.lpk` Release Asset can repair a missing store submission. The workflow verifies the GitHub asset digest and downloaded bytes before either store call; it never substitutes an unversioned or differently named LPK.

Secret scope is a workflow concern, not a configuration field. An organization Secret must authorize the repository. If the same name is defined more than once, Environment overrides Repository and Repository overrides Organization.
