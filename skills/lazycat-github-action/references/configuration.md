# Configuration reference

## Core shape

```yaml
version: 1
project:
  root: .
  build_config: lzc-build.yml
  package_file: package.yml
  output: dist/application.lpk
update:
  strategy: pull
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
  private:
    enabled: false
```

Unknown fields fail validation. Paths must remain under `project.root`. Output must end in `.lpk`.

## Version source

- `type: image`: Docker automation; `image` must name one configured image ID.
- `type: git`: tag/release/static/Exec/source builds; `image` must be empty.

The version source answers “which upstream version changes package.yml.” The image target answers “which Manifest field changes.” They are separate decisions.

## Channels

| Channel | Required behavior |
|---|---|
| `stable` | Highest non-prerelease SemVer; sort is `semver` |
| `beta` | Highest alpha/beta/rc/preview SemVer; sort is `semver` |
| `nightly` | `tag_regex` required; newest amd64 creation time; sort is `created` |
| `custom` | `tag_regex` and explicit `semver` or `created` sort required |

Use `exclude_regex` to remove Windows/ARM tags. `version_regex` must contain `(?P<version>...)`; `version_template` defaults to `{version}`.

Nightly mutable tags become deterministic SemVer values based on creation time and the `linux/amd64` digest.

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

Buildscripts receive version, tag, channel, source date, and fixed LazyCat target variables. They do not receive LazyCat credentials, private-store credentials, Registry credentials, GitHub tokens, or GitHub control-file paths.

Only local Docker/buildscript work requires Docker. OCI inspection, direct/mirror edits, and LazyCat remote Registry copying do not invoke local Docker.

## Official store

```yaml
stores:
  official:
    enabled: true
    create_if_missing: true
    changelog_locales: [zh, en]
    application:
      language: zh
      name: Example App
      source: https://github.com/acme/example
      source_author: acme
```

Defaults: locales `zh,en`; language `zh`; application name from `package.yml.name`. Application metadata is valid only with `create_if_missing: true`.

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
    name: Example App
    summary: Published from CI
```

Secrets: `APPSTORE_URL`, `APPSTORE_TOKEN`, optional `APP_ID`. The client creates an application with `POST /api/v1/apps` or creates an external version with JSON `POST /api/v1/apps/{id}/versions`. It always sends `sourceType: GITHUB`, GitHub Release Asset `downloadUrl`, and locally computed `sha256`.
