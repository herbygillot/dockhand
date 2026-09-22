# 2026-09-21: portedit/observe

## What moved

Step 4 of the [workspace design](../workspace-design.md), the second half of
the split the [architecture review](../reviews/2026-09-21-architecture-and-organization.md)
drew: `internal/macports/portedit/observe` holds the modeled observation of
a Portfile and the judgement of what its evaluation depended on.

- `Session` replaces the two runners that took the whole `sourceInput`:
  `Observe` observes contents in every profile at once, `One` in one, and
  `Profiles` closes over the platform boundaries the baseline and a
  candidate declare. A session knows the owning and selected targets, the
  native platform, the baseline contents whose declarations it caches, and
  a `Project` function that maps contents to the projection they are
  observed in; the projections' lifetime stays the caller's, and the
  operands the profile scan finds live on the session instead of the input.
  `WithBaseline` is a session for other baseline contents, which the
  dependency path's stripped form needs, with its own cache.
- The pure half moved unchanged: the platform scan and profile builder,
  the unmodeled-read classification, and the host-access judgement, with
  `Tolerate` and `HostInputs` as its exported names and `ErrInconclusive`
  as the sentinel `portedit` keeps under its old unexported name.
- `sourceInput` holds a session in place of its two observation fields,
  made at load once the baseline is known; the derived input in
  `dependencies.go` gets a session for the stripped contents.

## Sizes

| package | lines |
| --- | ---: |
| `portedit` | 2965 |
| `portedit/observe` | 1046 |
| `portedit/archives` | 409 |

The review projected 1,600 lines for `portedit` after a three-way split;
the orchestration it counted as leaf stays, and the honest figure is above.

## Evidence

The pure tests moved with the code; the observation cache test drives the
session directly, including a fresh session for other baseline contents;
the preparation, CLI, assess, and outdated suites run the observations end
to end under port-tclsh. No behavior changes.
