# Portedit: workspace behavior and an archive store

Implements the `portedit` items from the [structural review](../reviews/2026-09-16-bump-machinery-structure.md). No new package; the change stays inside `macports/portedit`, and `workflow/preparation`'s use of the exported `CheckEquivalent` is unchanged.

## Workspace

`workspace` was a one-field struct whose root was joined into paths at thirteen sites while two functions each wrote a candidate Portfile in place, evaluated, and restored the original. It now owns `path` and `withContents`, the write-run-restore cycle, and `sourceInput` gains `portfile`, `portdir`, and `context`, which binds the workspace to the owning Portfile or to the selected subport for a counterfactual probe. `evaluateContents` and `observeContents` are callbacks over `withContents`, and neither takes the request any more, since the only thing they used from it was the source, which the workspace carries. A copy of the session with another target, as the shared-release owner check makes, resolves its own paths.

The `(portfile.Edit, Snapshot, string, error)` quadruple is an `evaluation` struct with the edit and the snapshot. Its third value was the workspace root, returned unconditionally, so every before-and-after root pair threaded through `revisionFidelity`, `versionFidelity`, `checksumFidelity`, `scopedVersionFidelity`, and `scopedChecksumFidelity` was the same root; those comparators take one root now. `CheckEquivalent` keeps two, because `workflow/preparation` compares the workspace against a separately materialized stored tree.

## Archive store

Archives reached disk only when `Service.archiveDirectory`, an unexported field, was set by cloning the service inside dependency regeneration. `archiveStore` makes that explicit: `Service.archives(directory)` returns a store whose `fetch` keeps the bytes and records the path when a directory is given, removes a partial file on failure, and only hashes otherwise; `fetchFirst` tries locations in order, and `refresh` downloads every declared archive and rewrites the checksums. `applyArchivePlan` and `applyObservedArchives` take the store as a parameter, so the plain bump, the observed bump, and dependency regeneration share one download path with the directory choice visible at the call.

## Terminal tail

The four paths that ended by recording files, fidelity, a fidelity refusal, and one commit intent, the revision bump, checksum refresh, plain version bump, and observed version bump, now call `Result.commitEdit`. It records the attempted edit and fidelity before judging, so a refusal still reports what was tried, and it is the single place the commit subject is composed. The checksum refresh keeps its no-change early return.

Test-only helpers over the single-archive path, `downloadSource` and `Service.download`, moved into the download test file.

## Checks

- New tests: the workspace restores contents after a failing callback and refuses a missing file; session paths follow a copied target; the store keeps bytes only with a directory, leaves no partial file on failure, and takes the first working location; `commitEdit` records files and fidelity before judging and intends no commit on refusal.
- `go test ./... -count=1` and `go vet ./...`: passed. Two tests changed only in signature: the observation-reuse test no longer passes a request, and the revision-fidelity test uses one root, as the comparator now does.
- Real tree, rebuilt binary: `bump-revision deno --diff` produces the same diff; `bump deno 2.9.7 --diff` reaches the same unpublished-asset refusal through the store; `refresh-checksums deno --diff` is refused with "checksums must cover every source distfile exactly once" both before and after the change, built from the previous commit in a worktree for comparison. Deno declares per-architecture distfiles that the plain checksum path does not model; that is pre-existing and separate.
