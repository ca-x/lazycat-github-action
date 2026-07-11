# Version Template Named Groups

## Goal

Allow image tags whose version components need normalization, such as `20260603.01`, to map to a valid application SemVer such as `20260603.1.0` without repository-specific scripts.

## Contract

`version_regex` continues to require a named `version` group. Every named capture group becomes an optional `version_template` placeholder. For example:

```yaml
version_regex: '^(?P<version>\d{8})\.0*(?P<build>[1-9]\d*)$'
version_template: '{version}.{build}.0'
```

Existing templates containing only `{version}` remain byte-for-byte compatible. Repeated placeholders are allowed. Unmatched optional groups expand to an empty string and the final value must still be valid SemVer.

## Failure behavior

The mapping fails closed when the tag does not match `version_regex`, the required `version` group is absent, a placeholder remains unresolved, or the expanded value is not valid SemVer. Errors identify the mapped value and source tag without exposing credentials.

## Verification

- Preserve all existing version-selection tests.
- Add a mapping test for `20260603.01` to `20260603.1.0`.
- Add a failure test for an unknown placeholder.
- Document the syntax in README, configuration reference, and repository Skill.
