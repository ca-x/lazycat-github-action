# Release Notes Implementation Plan

> **For agentic workers:** Use executing-plans to implement this plan task-by-task.

**Goal:** Generate configurable application update notes, reuse exact-version GitHub Release bodies, document the parameters in the repository Skill, and publish v1.3.4.

**Architecture:** Resolve notes in the Go Action once the application version is known. The reusable workflow consumes the resolved `changelog` output for both GitHub Releases and stores. Explicit changelog text wins; a configured Go text/template receives matching Release notes and package metadata; the default template uses Release notes or falls back to `Release <version>`.

**Tech Stack:** Go 1.25, text/template, GitHub Releases REST API, composite Action and reusable workflow YAML.

**Spec:** User request on 2026-10-23, including the follow-up to document parameters in the Skill.

## Global Constraints

- Keep existing explicit `changelog` inputs literal and highest priority.
- Add `release.changelog_template`, `release.github_repository`, and `release.use_github_release_notes` (default true); add optional input `changelog-template` to both entry points.
- Infer upstream repository from package homepage, otherwise use the current GitHub repository. Explicit repository configuration overrides inference.
- Read only exact application-version tags (event tag, canonical v-prefixed version, bare version); never select latest or an unrelated version.
- Release-event bodies are usable without an API request; missing, empty, draft, inaccessible, or unavailable Releases fall back without failing publication.
- Validate templates and repository overrides before build/publish side effects. Never execute Release bodies as templates or shell code.
- Document all inputs, defaults, template variables, source precedence, and examples in the repository Skill and references.
- Release through the existing versioned tag workflow; verify CI, assets, and the floating v1 tag remotely.

## Review Focus

- Explicit text containing template syntax remains literal and bypasses lookups.
- Tag prefixes, component tags, prereleases, and version mismatches cannot fetch another version's notes.
- Whitespace-only or missing Release bodies still produce nonempty default notes; API errors do not expose credentials.
- Multiple-line Markdown and GitHub output delimiter collisions survive the entire path unchanged.
- The initial check's selected version drives store reconciliation notes and GitHub Release body alike.

## Tasks

- [x] Implement and test bounded GitHub Release lookup and Go-template rendering in `internal/releasenotes`; validate `release` configuration.
- [x] Integrate resolved notes with build/check/publish results, event parsing, Action/workflow inputs and outputs; test actual workflow data wiring.
- [x] Update README translations, Skill, configuration/workflow references, and examples; bump to v1.3.4; run race/vet/workflow/build/fixture checks and review.

Release follow-through: push the verified commit, create v1.3.4, and verify CI, Release assets, and the floating v1 tag through the existing release workflow.

## Review corrections

- Validate field references through the template AST instead of executing synthetic empty package data.
- Render selected image-version notes through `BeforeApply` before Manifest edits, using the same selected candidate.
- Reuse final API/event bodies only when a same-version Release in the current publication repository already contains the expected application LPK. Explicit per-run templates still override; manual Releases without that artifact provide raw template data.
- Preserve leading Markdown indentation and trailing newlines throughout resolution and store payloads.
- Bound notes and template source to 64 KiB UTF-8 for safe nested Action environment inputs; oversized automatic bodies fall back.
