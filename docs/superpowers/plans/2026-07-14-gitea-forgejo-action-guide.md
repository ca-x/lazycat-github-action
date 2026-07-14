# Gitea and Forgejo Action Guide Implementation Plan

> **For agentic workers:** Implement this plan task by task in the current workspace, then commit and push the verified documentation changes.

**Goal:** Add bilingual instructions for running the composite Action on Gitea Actions and Forgejo Actions, then link the appropriate guide from each README.

**Architecture:** Keep platform-specific CI instructions in two matching files under `docs/`. The root READMEs remain concise and point readers to the guide from the interface-selection section.

**Tech Stack:** Markdown, Gitea Actions workflow YAML, Forgejo Actions workflow YAML

## Global Constraints

- Document direct composite Action usage; do not claim that the GitHub reusable workflow is portable as-is.
- Use fully qualified GitHub action URLs in Gitea and Forgejo examples.
- Require Linux amd64 or arm64 runners with Bash, curl, tar, and sha256sum.
- Explain the GitHub Release bootstrap dependency and its mirror overrides.
- State that private-store publishing currently accepts only GitHub Release Asset URLs.
- Keep the English and Chinese guides structurally equivalent.

---

### Task 1: Add the bilingual platform guide

**Files:**
- Create: `docs/gitea-forgejo-actions.md`
- Create: `docs/gitea-forgejo-actions.zh-CN.md`

- [ ] Write matching English and Chinese sections for support status, requirements, project configuration, Gitea workflow examples, Forgejo workflow examples, operation support, bootstrap mirroring, known limitations, and troubleshooting.
- [ ] Cross-link the language variants at the top of both files.
- [ ] Check every workflow example against the current `action.yml` inputs and repository behavior.

### Task 2: Link the guides from the READMEs

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`

- [ ] Add one short note after the interface examples in each README.
- [ ] Link the English README to `docs/gitea-forgejo-actions.md` and the Chinese README to `docs/gitea-forgejo-actions.zh-CN.md`.
- [ ] State that Gitea and Forgejo users should call the composite Action directly.

### Task 3: Verify the documentation

- [ ] Run the Chinese and English punctuation checks.
- [ ] scan the new files for placeholders and invalid local links.
- [ ] Run `bash scripts/run-action_test.sh` to confirm the documented bootstrap requirements still match the tested script.
- [ ] Review `git diff --check` and the final diff.

### Task 4: Commit and push

- [ ] Re-read the current HEAD and worktree state before staging.
- [ ] Stage only the two guides, two README updates, and this implementation plan.
- [ ] Commit the documentation changes with a `docs:` commit message.
- [ ] Re-read HEAD and worktree state, push the current `main` branch, and verify that `origin/main` points to the new commit.
