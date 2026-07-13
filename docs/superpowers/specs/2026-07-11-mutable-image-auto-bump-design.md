# Mutable Image Automatic Version Bump Design

## Goal

Support applications whose upstream registry exposes only a mutable tag such as `latest`. When the selected `linux/amd64` image digest changes, the Action automatically bumps the current package patch version, updates the Manifest, builds a versioned Release Asset, and publishes configured stores. An unchanged digest is a no-op.

## Public configuration

Add one optional field to the image version source:

```yaml
update:
  strategy: publish
  allow_downgrade: false
  version_source:
    type: image
    image: app
    bump: patch

images:
  - id: app
    source: example/app
    channel: custom
    sort: created
    tag_regex: '^latest$'
```

`bump` accepts the single value `patch` in this release. Omitted behavior remains unchanged. It is invalid with `type: git`, an empty version-source image, a non-version-source image, SemVer-derived version mapping, or `allow_downgrade: true`.

## Selection and versioning

For `bump: patch`:

1. Filter tags using `tag_regex` and `exclude_regex`.
2. Select the newest usable `linux/amd64` candidate using `sort: created`.
3. Compare its digest with the digest represented by the current delivered image.
4. If equal, retain the current package version and report `changed=false`.
5. If different, strictly parse the current package version and increment only its patch component. Prerelease/build metadata is rejected for the first implementation rather than guessed.
6. Apply the bumped version only to the configured version-source image. Other managed images may update references without independently bumping the package.

The version bump happens after digest-change proof and before file writes. Downgrade logic remains unchanged for ordinary version mapping and is not used by bump mode.

## Digest state by delivery mode

- `direct`: write the runtime image as `<source>:<tag>@sha256:<digest>`. The digest-pinned Manifest is the durable previous-state record.
- `lazycat`: persist the selected source digest in the Manifest upstream comment (`source:tag@sha256:...`) and compare that baseline with the newly selected target-platform digest. Equal digests do not call the copy API. A legacy LazyCat runtime without a digest baseline performs one authenticated copy and compares the content-addressed returned LazyCat reference with the current runtime; an external runtime performs one migration copy without a version bump. The successful write establishes the pinned baseline for later runs. Never anonymously inspect the private LazyCat Registry.
- `mirror`: inspect the configured mirror and compare its digest. Bump mode requires `require_digest_match: true`; a missing or mismatched mirror fails without bumping.

Dry-run compares the persisted digest baseline and calculates the same bump decision without copying images, editing files, creating Releases, or querying stores. A legacy private LazyCat runtime without a persisted baseline fails closed until one trusted non-dry migration establishes it.

## Outputs and logs

Image results add enough information to audit the decision:

- current delivered digest;
- selected source digest;
- `digestChanged`;
- bump strategy;
- previous and selected package versions.

Structured logs report mutable-tag selection, digest comparison, no-op or bump decision, and the resulting version. Secret values and protected environment variables remain excluded.

## Failure behavior

| Condition | Result |
|---|---|
| Current package version is not strict stable SemVer | Fail before delivery or writes |
| Persisted digest baseline is invalid | Fail closed; do not bump |
| Legacy private LazyCat runtime is dry-run without a baseline | Fail closed; require one trusted migration run |
| Mutable source lacks `linux/amd64` | `VERSION_NOT_FOUND` |
| Mirror digest differs from source | `IMAGE_COPY_FAILED`; do not bump |
| Digest is unchanged | Successful no-op |
| Digest changed but delivery fails | Fail without file/version changes |

## Tests

- Config decoding, validation, unknown value rejection, and backward compatibility.
- Patch bump arithmetic and rejection of prerelease/build versions.
- Direct digest pinning and idempotent second run.
- LazyCat persisted-digest equality skip, changed-digest copy/bump, legacy authenticated baseline migration, and no anonymous private-registry inspection.
- Mirror equality, mismatch, and required verification.
- Dry-run parity without mutation.
- Version-source-only bump in multi-image projects.
- Logs and JSON outputs contain decisions but no credentials.
- Reusable workflow, README, Chinese README, configuration reference, workflow reference, Skill, test prompts, evals, and metadata contract tests.

## Rollout

Release as the next `v1.1.x` Action version, update floating `v1`, then configure EinkSync with direct delivery plus `bump: patch`. EasyNVR uses the verified current upstream version `7.7.406` as its migration baseline and then uses LazyCat delivery plus `bump: patch` for subsequent mutable `latest` digest changes.
