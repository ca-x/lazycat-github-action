# Image Tag Updated Sort Design

## Objective

Add an optional image sorting mode that follows the most recently updated Docker Hub tag instead of the highest SemVer. This covers upstream repositories that intentionally republish an older version tag, such as `zerodeng/sublink-pro:v1.2.15` being updated after `v1.2.26`.

The default remains `sort: semver`, so existing repositories do not change behavior.

## Public Configuration

Stable, beta, and custom image rules may explicitly set:

```yaml
channel: stable
sort: updated
```

`updated` means the Docker Hub tag metadata field `last_updated`, not the OCI image configuration field `created`.

- `semver`: highest mapped SemVer first.
- `updated`: newest Docker Hub tag update first, then highest mapped SemVer, then lexical tag order.
- `created`: newest `linux/amd64` OCI image configuration creation time first.

Nightly remains restricted to `created`. Stable and beta continue to default to `semver`. Updated sorting is initially supported only for Docker Hub repositories because the OCI Distribution API does not define a portable tag-update timestamp.

The project may also select its LazyCat target architecture:

```yaml
project:
  target_arch: arm64
```

`project.target_arch` is optional, defaults to `amd64`, and accepts only `amd64` or `arm64`. The target operating system remains Linux, so the effective platforms are `linux/amd64` and `linux/arm64`. Runner architecture remains independent: either Action binary may build or inspect either configured target when the project toolchain supports cross-building.

The target architecture is a project-wide invariant. It controls OCI manifest selection, mirror digest verification, LazyCat Registry copy requests, build environment variables, LPK/result metadata, and publication validation. Per-image architecture overrides are intentionally not supported because one LPK must have one coherent target platform.

## Architecture and Data Flow

1. Configuration validation accepts `updated` for stable, beta, and custom channels.
2. The registry adapter lists and filters registry tags as it does today.
3. For `updated`, the adapter reads paginated Docker Hub tag metadata, maps `last_updated` onto each eligible candidate, and rejects missing or zero timestamps.
4. Versioning maps tags to SemVer, filters stable/beta prerelease eligibility, and ranks candidates by update time, mapped SemVer, and tag.
5. The registry adapter inspects ranked tags in order until it finds the first usable `linux/amd64` image. This avoids fetching every manifest merely to rank by a timestamp already supplied by Docker Hub.
6. The selected candidate continues through the existing delivery, Manifest editing, versioning, build, Release, and store flows.

Docker Hub metadata requests use a bounded response, context cancellation, an HTTP timeout, a maximum of 10,000 tags, and status-only errors. Remote response bodies are not included in diagnostics.

Target architecture is normalized once at the configuration boundary and then passed explicitly as a `platform.Target` value. It is never stored in mutable package-level state, so tests and concurrent callers cannot leak architecture choices into each other.

## Downgrade and Mutable-Tag Behavior

`sort: updated` changes candidate ordering but does not silently disable `update.allow_downgrade: false`.

- If the selected mapped version equals the current package version, the existing equal-version image refresh path remains allowed.
- If the selected mapped version is lower, the existing `VERSION_DOWNGRADE_BLOCKED` result remains in force unless the repository separately and explicitly enables `allow_downgrade: true`.
- LazyCat delivery treats `updated` like `created` for refresh decisions so a repushed tag can be recopied even when its textual source reference is unchanged.

For `lazycat-contrib/sublink-pro-lzcapp`, the current package version is already `1.2.15`, so selecting recently updated tag `v1.2.15` does not require downgrade authorization.

## Alternatives Considered

### Reuse `sort: created`

Rejected. OCI `config.created` records image build metadata and may remain old when a tag is moved or republished. It is not the Docker Hub tag update time shown to users.

### Add `prefer_updated: true`

Rejected. Candidate ordering is already represented by `sort`; a second boolean would create conflicting states such as `sort: semver` plus `prefer_updated: true`.

### Infer update preference automatically

Rejected. That would change existing stable repositories and make version selection depend on mutable remote timestamps without an explicit opt-in.

## Testing Strategy

- Configuration contract tests cover the unchanged default, accepted channel combinations, and invalid nightly use.
- Versioning tests prove primary update-time ordering and deterministic SemVer/tag tie-breaks.
- Docker Hub metadata tests cover pagination, URL escaping, missing timestamps, tag limits, cancellation, non-2xx responses, and bounded JSON.
- Registry tests prove ranked inspection skips a newer arm64-only tag and stops at the first usable `linux/amd64` candidate.
- Image-flow tests prove the downgrade guard remains active and updated sorting triggers mutable-tag delivery refresh.
- Platform, registry, delivery, build, LPK, publish-flow, and Action tests prove an explicit arm64 target reaches every downstream consumer while omission remains amd64.
- Metadata tests require README and Skill coverage.
- Full Go, race, vet, staticcheck, shell/action metadata, and actionlint checks run before release.
- A released `v1` is exercised through `lazycat-contrib/sublink-pro-lzcapp`; logs must show `sort=updated` and selection of the tag with the newest Docker Hub `last_updated` timestamp.

## Success Criteria

- Existing configurations continue to select by SemVer.
- `sort: updated` selects the most recently updated eligible Docker Hub tag.
- An unavailable or unsupported update timestamp fails explicitly rather than falling back to OCI creation time.
- Target platform filtering always matches the normalized project target.
- The omitted target architecture remains `linux/amd64`; explicit `target_arch: arm64` consistently produces `linux/arm64` inspection, copy, build, validation, and output metadata.
- Downgrade protection remains enabled by default.
- README, Chinese README, configuration reference, Skill guidance, and evals explain the new mode.
- The next patch release, floating `v1`, GitHub Release assets, and Marketplace entry are verified.
