# Image Version Downgrade Guard Design

## Context

The registry adapter already uses `github.com/google/go-containerregistry` to list tags and inspect `linux/amd64` manifests, digests, and creation times. A `custom` image rule with `sort: created` can nevertheless choose a recently rebuilt older release. Odoo demonstrated this when rebuilt tag `18.0` was newer by creation time than `19.0`, causing the Action to rewrite application version `19.0.0` to `18.0.0`.

## Decision

Add optional configuration field `update.allow_downgrade`, defaulting to `false`. After the version-source image has been selected and mapped through `version_regex` and `version_template`, compare its SemVer with the current `package.yml.version`.

- A lower selected version fails before image delivery, Manifest edits, package edits, Release creation, commits, or store publication.
- An equal version remains allowed so a mutable tag or rebuilt image digest can refresh the Manifest without changing the package version.
- A higher version follows the existing flow unchanged.
- Repositories that intentionally move to a lower SemVer must explicitly set `allow_downgrade: true`.
- Images that do not drive `update.version_source` are not compared with the application version.

The Action reports a distinct `VERSION_DOWNGRADE_BLOCKED` error containing only the selected and current versions. No registry credentials, store credentials, response bodies, or image-copy tokens are included.

## Alternatives

1. Force every `created` rule to sort by SemVer. Rejected because nightly and mutable channels intentionally need creation-time ordering.
2. Compare only with store versions. Rejected because store lookups occur after image delivery/build, private applications can require group visibility, and pending official submissions are not anonymously visible.
3. Silently keep the current version. Rejected because it hides a broken selection rule and can leave operators believing the newest image was published.

## Verification

- Contract-test the new YAML field and default.
- Reproduce an Odoo-style `19.0.0 -> 18.0.0` selection and prove delivery/Manifest writes do not occur.
- Prove `allow_downgrade: true` permits the same transition.
- Prove equal-version digest refresh remains allowed.
- Run unit tests, race, vet, action metadata tests, actionlint, ShellCheck, bootstrap smoke tests, and release snapshot verification.
