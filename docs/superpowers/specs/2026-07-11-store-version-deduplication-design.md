# Store Version Deduplication Design

## Goal

Upgrade `github.com/lib-x/lzc-toolkit-go` to `v0.2.0` and optionally skip official LazyCat App Store or Miaomiao private App Store publishing when the store already reports the same latest version for the exact LPK package ID.

The change must remain backward compatible: existing configurations continue publishing without a preflight lookup unless the new store-specific option is explicitly enabled.

## Dependency Boundary

The Action will use the two read-only packages introduced by `lzc-toolkit-go v0.2.0`:

- `appstore/official` for anonymous official-store application metadata lookup.
- `appstore/private` for anonymous Miaomiao latest-version lookup with optional private group codes.

The existing authenticated developer-platform package `appstore` remains responsible for official submission and image copy. The Action's existing private-store publishing client remains responsible for Miaomiao write operations. Read and write protocols stay separate so anonymous lookup never receives a publishing token.

## Configuration Contract

Each store receives an independent opt-in flag:

```yaml
stores:
  official:
    enabled: true
    skip_if_version_exists: true
  private:
    enabled: true
    skip_if_version_exists: true
```

Both flags default to `false`. Existing repositories therefore retain the current behavior after upgrading to `v1.1.0`.

The option means "skip when the latest visible online version string exactly equals the verified LPK version." It does not compare file hashes, changelogs, timestamps, or version ordering.

## Private Group Codes

Miaomiao private group codes are credentials and are never part of `.github/lazycat-action.yml` or ordinary reusable-workflow inputs.

The reusable workflow exposes an optional secret named `PRIVATE_STORE_GROUP_CODES`. Direct composite-Action callers provide the same value as an environment variable backed by a GitHub Secret:

```yaml
- uses: ca-x/lazycat-github-action@v1
  env:
    PRIVATE_STORE_GROUP_CODES: ${{ secrets.PRIVATE_STORE_GROUP_CODES }}
```

The value is a comma-separated list. The Action trims entries and passes them to `appstore/private`; normalization, validation, deduplication, the default `X-Group-Codes` header placement, Cookie isolation, and redirect blocking remain owned by the toolkit.

The Action must never write group codes to GitHub outputs, step summaries, result JSON, errors, or logs.

## Publish Data Flow

The existing flow continues to validate the local LPK, package ID, version, target platform, and expected Release Asset SHA256 before any store interaction.

When `skip_if_version_exists` is disabled, publishing follows the current path without a new lookup.

When enabled, the flow performs an anonymous lookup after artifact validation and before resolving publishing credentials:

1. Query the selected store using the exact verified package ID.
2. If the application is not found, continue with normal publishing.
3. If the latest visible version differs, continue with normal publishing.
4. If the latest visible version exactly equals the verified LPK version, return a successful skipped result without resolving a token or calling a write endpoint.
5. If lookup fails for any reason other than not-found, fail the operation. An explicitly enabled deduplication guarantee must not silently fall back to a possibly duplicate submission.

Official lookup uses the toolkit's default public official-store metadata endpoint. Private lookup uses the existing `APPSTORE_URL` store origin and optional `PRIVATE_STORE_GROUP_CODES` secret.

`dry-run` preserves its current network-free behavior. It reports the planned publication but does not query either store and therefore cannot report an online-version match.

## Result Contract

The existing `store-results` JSON remains the single output. Store result objects gain additive fields:

- `skipped`: `true` only when an equal latest version prevented submission.
- `onlineVersion`: the normalized latest version returned by the store when a lookup succeeded.

For an equal-version skip:

- `published` is `false`.
- `skipped` is `true`.
- `version` remains the verified LPK version.
- `onlineVersion` equals `version`.
- Existing identity fields such as `packageId`, SHA256, or download URL remain populated where they are already part of that store's result contract.

Normal publishing retains `published: true` and `skipped: false`. Additive JSON fields preserve compatibility with existing consumers.

## Interfaces and Ownership

`config` owns the two YAML booleans and their default/validation behavior.

`githubio` owns reading `PRIVATE_STORE_GROUP_CODES` without exposing it in outputs.

`publishflow` owns the store-neutral decision: disabled, not found, version differs, version matches, or lookup failure. It receives narrow lookup functions through `Flow` dependencies so all branches can be tested without network access.

The official store adapter translates `appstore/official.Application` into the latest online version string. The private lookup adapter constructs `appstore/private.Options`, passes group codes, and translates `LatestVersion` into the same internal lookup result. Existing publishing adapters remain unchanged except for additive result fields.

## Error and Security Behavior

- External responses remain untrusted and are validated by `lzc-toolkit-go v0.2.0` before the Action compares versions.
- Package identity must match the exact verified LPK package ID.
- A not-found error is the only lookup error that permits publishing to continue.
- Group codes and publishing tokens must never cross between read and write clients.
- Official lookup remains anonymous and does not trigger platform credential resolution.
- Equal-version private lookup does not require `APPSTORE_TOKEN`; the write token is resolved only if publishing continues.
- Existing safe error mapping remains in place so remote response bodies and secrets are not surfaced.

## Tests

Configuration tests cover both flags, default-off behavior, normalization, and strict YAML field handling.

Publish-flow tests cover each store independently:

- option disabled performs no lookup;
- equal latest version skips before authentication or publishing;
- different latest version continues publishing;
- not found continues publishing;
- malformed or unavailable lookup fails closed;
- dry-run performs no lookup;
- private group codes reach the toolkit lookup adapter;
- skipped results serialize through `action.Result.StoreResults` and GitHub outputs.

Metadata tests verify the reusable-workflow secret declaration and confirm no ordinary Action input exposes group codes. Existing race, vet, actionlint, ShellCheck, fixture, cross-build, snapshot-release, archive-content, and security scans remain release gates.

## Documentation and Release

Both READMEs and the repository Agent Skill document:

- the two opt-in flags;
- exact string equality semantics;
- fail-closed lookup behavior;
- the `PRIVATE_STORE_GROUP_CODES` secret and direct-Action environment binding;
- network-free `dry-run` behavior;
- additive skipped-result fields.

This backward-compatible feature release is `v1.1.0`. `action.yml` embeds `v1.1.0`. After local and GitHub CI gates pass, create and push annotated tag `v1.1.0`, verify the GitHub Release assets, checksums, SBOMs, binary version metadata, and provenance, then move annotated floating tag `v1` to the same release commit.

The floating tag must not move until the immutable `v1.1.0` release is verified.
