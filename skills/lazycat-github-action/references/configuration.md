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
    skip_if_version_exists: false
  private:
    enabled: false
    skip_if_version_exists: false
```

Unknown fields fail validation. Paths must remain under `project.root`. Output must end in `.lpk`.

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
    skip_if_version_exists: true
    create_if_missing: true
    changelog_locales: [zh, en]
    application:
      language: zh
      name: Example App
      source: https://github.com/acme/example
      source_author: acme
```

Defaults: locales `zh,en`; language `zh`; application name from `package.yml.name`. Application metadata is valid only with `create_if_missing: true`.

`skip_if_version_exists` defaults to false. When true, the Action anonymously queries the exact package after LPK verification and skips an equal latest version before resolving official credentials. Not-found continues; other lookup errors fail closed. `dry-run` does not query.

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

`skip_if_version_exists` has the same default-off, exact-equality, not-found, fail-closed, and network-free dry-run behavior as the official option. The write client creates an application with `POST /api/v1/apps` or creates an external version with JSON `POST /api/v1/apps/{id}/versions`. It always sends `sourceType: GITHUB`, GitHub Release Asset `downloadUrl`, and locally computed `sha256`.

With scheduled `publish` automation, an existing exact `<package-id>-v<version>.lpk` Release Asset can repair a missing store submission. The workflow verifies the GitHub asset digest and downloaded bytes before either store call; it never substitutes an unversioned or differently named LPK.

Secret scope is a workflow concern, not a configuration field. An organization Secret must authorize the repository. If the same name is defined more than once, Environment overrides Repository and Repository overrides Organization.
