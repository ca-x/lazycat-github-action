# GitHub Release Retention Implementation Plan

> **For agentic workers:** Use the executing-plans skill to implement and verify each task without publishing or deleting live releases.

**Goal:** Add independently configurable GitHub Release cleanup, enabled by default and retaining the newest 1000 published versions.

**Architecture:** Read `release.cleanup.enabled` and `release.cleanup.keep_latest` from project configuration and export normalized Action outputs. The reusable workflow cleans its resolved publication repository only after a verified Release asset is available and publication work has succeeded. Collect all Release pages before deleting old releases; preserve drafts, the current tag, GitHub's latest release, and every Git tag.

**Tech Stack:** Go, YAML v3, GitHub Actions, Octokit, Node fixture tests.

**Spec:** The current user request explicitly requires an on/off option, default enabled, a configurable retention count defaulting to 1000, and synchronized Skill guidance. Cleanup affects GitHub Releases and their assets, not either store's own version retention.

## Global Constraints

- No live cleanup, push, tag, or release during implementation.
- Upstream Docker tag discovery is a separate task with an independent plan-review gate.
- `release.cleanup.enabled: false` prevents cleanup API calls.
- Omitted `keep_latest` means 1000; an explicitly nonpositive value is rejected.
- Skip cleanup for dry runs, PRs, pending official review, and unsuccessful publication.
- Operate only on the resolved Release destination, using its existing publication token.
- Count published releases, including prereleases, by publication time rather than SemVer or list position.
- Drafts and protected current/latest releases can make the retained total larger than the requested count.
- Never delete Git tags or call either store's deletion APIs.
- Cleanup failures are visible but do not roll back a successful publication.

## Review Focus

- Listing must finish before deletion so page offsets cannot skip releases during cleanup.
- Invalid IDs/timestamps or a missing protected current Release must stop before any deletion.
- Cross-repository publication must not clean the private source repository.
- Disabled cleanup, dry runs, and PRs must make no cleanup calls.
- API failures must not expose response bodies or credentials.

### Task 1: Configuration and Action outputs

- [x] Add failing configuration tests for defaults, explicit disable/custom count, and invalid counts.
- [x] Add `ReleaseCleanup` and normalized `release-cleanup-enabled` / `release-keep-latest` outputs.
- [x] Verify configuration, Action, and output tests.

### Task 2: Reusable workflow cleanup

- [x] Add failing Node fixture tests against the real workflow script for pagination, ordering, protection, repository scope, and errors.
- [x] Add the cleanup step after publication, with safe gates and the resolved publication credentials.
- [x] Verify default retention at 1000, alternate counts, explicit disable, and no tag deletion.

### Task 3: Documentation and Skill

- [x] Document defaults, configuration, repository scope, protected versions, and asset deletion in both READMEs and Skill references.
- [x] Add Skill eval scenarios for enabled/disabled cleanup and custom retention.
- [x] Synchronize the installed Skill without overwriting unrelated user changes.
- [x] Run focused tests, workflow lint, and the relevant broader regression suite.
