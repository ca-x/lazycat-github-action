# Go Template Manifest Compatibility Design

## Problem

LazyCat Manifests may contain standalone Go Template control lines such as
`{{ if ... }}`, `{{ else }}`, and `{{ end }}`. The LPK builder preserves these
templates, but `lazycat-github-action v1.1.0` parses the source Manifest as
plain YAML during project inspection and image editing. A valid templated
Manifest therefore fails before image discovery with `INVALID_MANIFEST`.

The failure was reproduced against
`lazycat-contrib/nowledge-mem-lzcapp`: removing only its standalone `if` and
`end` lines makes project inspection succeed.

## Scope

Support Manifests whose standalone Go Template control lines wrap YAML that is
otherwise structurally valid when all wrapped nodes are present. Preserve the
original template lines exactly when the Action updates image fields.

This patch does not evaluate templates, invent deployment parameter values, or
attempt to merge mutually exclusive YAML branches that produce duplicate or
otherwise invalid YAML when viewed together.

## Design

Add one internal template-protection component shared by project inspection
and Manifest image editing.

Before parsing, it will:

1. Detect standalone Go Template control actions after trimming whitespace and
   optional Go Template trim markers.
2. Replace each supported control line with a unique YAML comment marker while
   retaining its indentation.
3. Keep an ordered mapping from marker to the exact original line.

Supported standalone controls are `if`, `else`, `end`, `with`, and `range`.
Inline template expressions such as `PASSWORD={{.U.password}}` remain
untouched because they are already valid YAML scalar content.

Project inspection parses the protected bytes only for structural discovery;
it never writes them. Manifest image editing parses and updates the protected
document, encodes YAML, then replaces every marker comment with its exact
original template line before the existing atomic file replacement.

Protection fails closed if the source already contains the reserved marker
prefix, a marker is lost or duplicated during encoding, or the protected
Manifest is still invalid YAML. These errors identify template preservation as
the failing stage without logging credentials or deployment values.

## Compatibility and Security

- Plain YAML behavior and formatting remain unchanged apart from the existing
  image-editor formatting behavior.
- Template expressions are never executed, so untrusted project templates
  cannot run code or access Action environment variables.
- Existing symlink, project-boundary, scalar-image, and atomic-write checks
  remain in force.
- Exact original control lines, including whitespace and trim markers, are
  restored after editing.

## Verification

Add public-behavior regression tests that prove:

1. `project.Inspect` accepts a service Manifest with standalone template
   controls and still classifies it as a service project.
2. `manifestedit.Read` finds explicitly configured images in that Manifest.
3. `manifestedit.Apply` updates only the requested image and upstream comment
   while preserving every template control line exactly.
4. Plain YAML behavior remains covered by the existing suite.
5. Unsupported/structurally invalid template layouts still fail without
   partially rewriting the Manifest.

Run focused tests first, then the full race, vet, actionlint, ShellCheck,
cross-build, snapshot-release, dependency, and diff gates before releasing
`v1.1.1`. Verify the Release archives, checksums, version metadata,
attestations, and annotated `v1`/`v1.1.1` tags before retrying the real
`nowledge-mem-lzcapp` dry-run.
