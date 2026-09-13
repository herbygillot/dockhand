# Source selection and explicit bump versions — 2026-09-13

## Decisions recorded

- Recorded the approved user experience: branches as contribution handles, ordinary Git edits, current-checkout verification with frozen working-tree contents, explicit committed-branch selection, and target inference only when scope is clear.
- Distinguished standalone verification from contribution tracking and edited ports from verification targets. The current one-branch/one-target limitation is explicitly an implementation constraint to remove.
- Specified source summaries before expensive work, staged additions and relevant untracked-file notices, immutable accepted inputs, and job-bound wait/cancel behavior.
- Recorded evidence applicability across identical source trees and matching build inputs, including committing already-tested edits. Publication still requires committed source and applicable coverage.
- Added `bump <target> [version]`: no version requests the latest eligible release; an explicit version can omit a prefix inferred from the current upstream reference convention. Keep requested text, Portfile version, and confirmed upstream tag distinct; ambiguous or unavailable resolution stays visible.
- Aligned intended command examples and package responsibilities in the CLI, architecture, and component documents. The existing implementation sections remain explicit about committed-only verification and job-ID wait/cancel.

## Scope and provenance

Documentation only. No command parser, executor, state schema, package, or dependency was changed. All text was authored for v2 from the approved discussion; no v1 comments or tests were copied. No commit was requested for this update.

## Validation

Reviewed the changed command examples, implementation-versus-design labels, and relative document links. `git diff --check` passed. No runtime tests were needed for this documentation update.
