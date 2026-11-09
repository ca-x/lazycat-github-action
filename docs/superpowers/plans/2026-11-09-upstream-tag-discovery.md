# Large Upstream Tag Discovery Implementation Plan

> **For agentic workers:** Use executing-plans to implement this reviewed plan; do not publish or change consumer repositories.

**Goal:** Discover the correct upstream image version without repeatedly increasing raw or matching tag counts as upstream repositories grow.

**Architecture:** Use the existing go-containerregistry pager, filter each page, retain unique matching names, then rank the complete result before inspecting manifests. Omitted/zero numeric limits mean automatic discovery governed by a 10-minute overall deadline, a 32 MiB retained-state accounting budget, and a default 4 MiB tag-page body limit. Explicit numeric caps remain optional hard limits. Docker Hub update metadata follows its own validated pagination under the same remaining deadline and retained-state budget.

**Tech Stack:** Go, go-containerregistry, Docker Hub API, existing SemVer mapping/ranking.

**Spec:** The user's Magpie workflow fails on 10277 upstream GHCR tags before filtering. Both GHCR and Docker Hub need automatic handling; increasing 10000 to another fixed tag count is not the requested root fix. GitHub Release retention is independent.

## Plan Review

- Independent agent Anscombe rejected the first proposal because an implicit 10000 matching-tag cap still required manual adjustment.
- Revised resource-budget automatic discovery received conditional Go, incorporating final-next URL validation, complete-body bounds, bounded cursor tracking, and a shared deadline.

## Global Constraints

- Both `max_tags` and `max_matching_tags` omitted/0 mean automatic; explicit positive values retain the existing maximum of 50000.
- Raw caps count all returned records, including duplicates; matching caps count unique names after include/exclude filtering.
- The 32 MiB budget charges each retained unique tag/cursor by byte length plus 64 bytes; it is not a total process-memory guarantee.
- Budget limits apply even with explicit numeric caps.
- Read every Next page, including after short/empty pages, before ranking or manifest inspection.
- Validate final Next URLs, including JSON Next values, against the same scheme, host, and repository tag path before following them; reject loops.
- The default production transport reads the whole decoded tag-page body up to 4 MiB + 1 and rejects oversized responses. Preserve explicit remote transport overrides.
- Any timeout, cancellation, pagination, or resource-budget failure returns an error, never partial candidates.
- Preserve version mapping, downgrade guards, complete SemVer ranking, and platform fallback; never keep only the first/top N tags.
- GHCR tag lists have no update timestamp. Docker Hub `updated` uses `last_updated`; OCI `created` remains image creation time.
- Hub metadata follows validated same-path `next` URLs until all desired timestamps are present or pagination ends; Count/short-page length cannot terminate discovery.

## Review Focus

- A highest version on the final page must win with 10001 and 50001 valid versions.
- Unmatched tags and duplicate candidate names must not consume matching-count limits.
- JSON Next must not bypass the upstream pager's Link-only safety checks.
- Valid JSON followed by oversized trailing bytes must not bypass the body limit.
- Late HTTP errors or cancellation must prevent all manifest requests.

### Task 1: Registry discovery

- [x] Add failing paginated fixtures for large automatic discovery, explicit limits, duplicates, late highest versions, exclude rules, and platform fallback.
- [x] Add shared budget, deadline, safe cursor tracking, and default bounded transport helpers.
- [x] Replace raw List with filtered pagination and remove implicit matching count defaults.
- [x] Verify response bounds, cycles, invalid Next paths, cancellation, and no partial results.
- [x] Reproduce and fix the independently reported redirect bypass; reject redirects outside the same tag-page origin/path and verify same-path redirected bodies remain bounded.

### Task 2: Docker Hub metadata

- [x] Add fixtures for huge Count, short pages with next, late requested tags, loops, invalid URLs, and missing timestamps.
- [x] Remove Count/matching-based page bounds; follow same-origin next with the shared resource limits.
- [x] Verify existing update-time ranking and safe error behavior.

### Task 3: Configuration, docs, and Skill

- [x] Document automatic zero/omitted limits and optional explicit hard caps with unchanged positive validation.
- [x] Add Skill scenarios for large upstreams and prevent manual count inflation or arbitrary list truncation as the default repair.
- [x] Synchronize both READMEs and the installed Skill, then run regression tests and review the diff.
