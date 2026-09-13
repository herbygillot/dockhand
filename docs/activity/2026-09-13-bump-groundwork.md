# Bump preparation groundwork — 2026-09-13

## Completed

Committed the approved source-selection and explicit-version design as `84b9e03` (`docs: define source selection and explicit bump versions`) before starting implementation.

- Replaced the preparation placeholder with a service that accepts immutable source/selection inputs, materializes the complete ports tree, resolves and evaluates the target and siblings, prepares a candidate tree, and validates the resulting evaluation. It returns source/target identities, file preconditions, commit intent, and fidelity observations without adopting a branch or writing workflow state.
- Implemented revision increments for a single literal revision command, missing default revisions, and literal selected subport blocks. Preserve surrounding source, including comments and line endings. Refuse unsupported expressions, ambiguous scopes, unintended sibling changes, changed metadata, and failed candidate evaluation. Calculated versions remain MacPorts' responsibility.
- Added Git file reads, immutable tree editing with content/existence/mode preconditions, auxiliary-file additions/deletions, and tree diffs. All preconditions are checked before edits are written. Unchanged subtrees retain their objects. Ref/index/worktree mutation and a second locking mechanism are unnecessary for this capability.
- Connected `bump-revision <port> --diff`, with explicit branch, subport, variants, reason, and JSON options. This first preview uses committed source and says so. It opens no SQLite database and needs no Tart image/provider. Non-preview preparation still returns an explicit not-implemented error without creating state.
- Added optional positional version parsing for `bump`, plus deterministic upstream matching of requested versions against supplied release evidence. Preserve explicit prefixes, infer arbitrary known prefixes/suffixes, distinguish Portfile version from upstream tag, and refuse missing/ambiguous/inconsistent matches. Current-version/current-tag inference requires one identifiable occurrence of the version. Computed Git/GitHub source options are now available from the MacPorts evaluator.

## Organization and provenance

All new implementation and tests were authored for v2. Consulted v1's preparation, source-location, and upstream-coordinate mechanisms for lessons about source context, edit corroboration, subports, and tag conventions. No v1 code, comments, or tests were copied. Existing v2 Git object/materialization, Tcl syntax/text edits, MacPorts evaluation, Cobra, and testify components were reused.

Kept the work in `git`, `prepare`, `upstream`, `macports`, `app`, and `cli`. Removed the unused preparation-specific file-edit duplicate in favor of Git's physical edit shape. Commit intent retains message/path choices; author identity can be resolved when branch integration is implemented. No new package, module dependency, state table, or migration was introduced.

## Validation

The full Go suite, affected-package race checks, `go vet ./...`, and CLI build passed. Tests cover immutable file preconditions, traversal/symlink/overlap refusals, unchanged subtrees and checkout/index contents, revision scope, defaults, calculated versions, sibling and metadata fidelity, explicit version parsing, ambiguous tags, arbitrary prefixes/suffixes, and JSON/preview isolation. Native MacPorts integration tests exercised the full preparation path. After the final release-matching refinement, the upstream and MacPorts tests were rerun.

A compiled CLI successfully previewed both of these against committed source in the user's MacPorts tree at `11f22962ec196b85d04fd400eb13d7c1751b5c47`:

```sh
dockhand bump-revision jq --diff
dockhand bump-revision terraform --subport terraform-1.16 --diff
```

Each proposed only its selected revision changing from 0 to 1. Terraform's calculated versions and sibling metadata remained unchanged. The ports checkout's existing untracked `aqua/rockxy/` and `editors/txt/` directories were preserved, and the supplied temporary database path was not created. No build or publication was requested. `git diff --check` passed.

## Remaining implementation

Version-tag matching is a capability, not a connected discovery command. Automatic latest-release discovery, concrete release readers, version-source edits (including calculated carriers), distfile fetching/checksums, and auxiliary-file generation remain unfinished. Version bump and checksum-refresh previews therefore return explicit unsupported-executor errors rather than an incomplete successful preparation.

The workflow still has no preparation executor, guarded branch integration/recovery, or preparation-to-verification continuation. The approved working-tree verification and standalone/contribution association changes also remain pending. This slice establishes a tested preparation boundary without building on the current exclusive branch/target restriction.
