# Store Online-Version Downgrade Guard Design

## Problem

Store reconciliation currently skips only when the online version exactly equals the verified LPK version. If the store already exposes a newer SemVer, the Action still uploads the older LPK. The official platform then rejects review creation with an opaque HTTP 400. EasyNVR reproduced this with online `7.8.138` and candidate `7.7.406`.

## Decision

When `skip_if_version_exists: true` performs a successful store lookup, compare valid SemVer values before resolving write credentials:

- equal versions: keep the existing successful skip;
- online version newer than the verified LPK and `update.allow_downgrade: false`: successfully skip only that store;
- online version newer and `update.allow_downgrade: true`: continue publishing because the user explicitly authorized rollback;
- candidate version newer: continue publishing;
- either value is not valid SemVer: retain exact-equality behavior and do not guess an ordering.

The successful downgrade-protection skip returns `published: false`, `skipped: true`, the observed `onlineVersion`, and a machine-readable `skipReason` of `online-version-newer`. Existing equal-version skips use `skipReason: version-already-online`.

## Scope

The guard applies independently to official and private stores. A protected official-store skip must not fail a workflow whose Release and private-store publication succeeded. It does not change image-selection downgrade protection, LPK verification, store authentication, or the default value of `skip_if_version_exists`.

## Logging and errors

The Action logs the store, candidate version, online version, and skip reason. Lookup not-found still permits publishing; other lookup failures remain fail-closed. No credential or upstream response body is logged.

## Testing

Publish-flow tests cover equal, newer-online, newer-candidate, explicitly allowed downgrade, and non-SemVer values for both store result types. Metadata tests require README and Skill documentation to describe the behavior. The full Go test, race, and vet gates must pass before release.
