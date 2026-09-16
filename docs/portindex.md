# PortIndex storage and reuse

Status: agreed design direction, recorded 2026-09-16; consolidation is not yet implemented. The [roadmap](roadmap.md) owns implementation priority.

## Disposition

Maintain a reusable PortIndex set for upstream MacPorts master, advance it incrementally as master changes, and copy a matching generation into each candidate workspace. Reindex the candidate's changed port directories, then reuse that exact candidate index for subsequent consumers.

Update the master index on demand when an operation needs an index for a newly fetched source. This does not require a background updater, a new daemon, or another cache package.

## Current arrangement

Parts of this mechanism already exist in `internal/macports/portindex`, but their storage and selection paths are fragmented:

- Name lookup, assessment, and outdated discovery use the system user cache: on macOS, `~/Library/Caches/dockhand/indexes`.
- Tart staging and dependent discovery use `indexes` beneath the Tart artifact directory: by default, `~/.dockhand/artifacts/tart/indexes`.
- The package separates standalone, complete, baseline, and candidate entries. Its rolling seed hint currently serves standalone indexing; a candidate baseline does not generally advance from the previous master generation.
- Cache profiles currently include the indexer executable digest, mirror URL, and target platform. A profile lock covers generation and staging.

Consequently, compatible consumers can repeat full indexing of the same source. The earlier cold Terraform exercise generated full indexes in both discovery and Tart staging. Consolidation should remove this duplication while retaining exact-source checks and incremental coverage validation.

## Normal bump flow

1. **Freeze the starting source.** Fetch upstream master and capture its exact commit and tree. That snapshot becomes the contribution's base; later movement of master does not change an accepted job.
2. **Obtain the matching master index.** Reuse an existing exact generation when available. Otherwise compare the prior cached source tree with the requested master tree and update the affected port directories. If no safe seed exists, generate a full index.
3. **Prepare the candidate index.** Copy the matching baseline into a private candidate workspace. Compare the baseline and candidate trees and regenerate entries for all affected port directories, including subports. Remove entries for deleted ports and handle renamed directories through the complete diff.
4. **Reuse the completed candidate generation.** Name lookup, dependency discovery, and verification staging request the same source-and-environment identity. A later stage does not build another index merely because a different component requested it.

No index is generated for an operation that does not need one. Multiple verification attempts against the same tree and compatible indexing environment share the completed generation, while each staged source receives its own copy.

Manual branches, historical sources, and captured working trees use this same mechanism. A master generation is a useful seed, not a requirement that their source be rebased or replaced.

## Identity and storage

A completed generation is identified by **source tree plus indexing environment**. Its identity does not depend on the requesting command, verification provider, contribution branch name, or which seed produced it.

The indexing environment must distinguish target OS/architecture and the relevant MacPorts/indexer runtime and configuration. Hashing only a launcher must not overlook changes to the Base code it loads. Source-bound PortGroups are covered by the source tree. Host facts that affect generation must either be controlled or represented in compatibility checks.

Use one shared user-cache root for all consumers, retaining the existing macOS discovery location as the default. This cache is independent of the SQLite database and Tart's artifact directory. Index-only commands must continue to work without opening operational state.

Store completed generations with their source and environment metadata. A small pointer, scoped to the upstream and indexing environment, identifies the preferred current-master generation. It is a seed-selection hint; it cannot substitute for an exact source match. Older generations may remain available for accepted jobs and reuse.

The mirror address is bootstrap provenance, not by itself a reason to duplicate otherwise equivalent completed generations. Cached entries must retain enough provenance and coverage information to support that equivalence. An index file's mere existence does not certify complete or usable coverage.

This is a PortIndex consolidation, not a relocation of all Dockhand assets. VM disks, verification logs, publication locks, and Git objects keep their separate ownership.

## Incremental invalidation and bootstrap

Use the full Git tree diff to identify affected directories. Preserve the existing checks for missing changed ports/subports and unexpectedly lost unchanged entries. Existing unrelated index failures must remain visible; an index is discovery metadata, not proof that every port builds.

Initially, changes under shared `_resources` require a full rebuild because PortGroups can affect unrelated ports. An incompatible indexing environment, missing source objects needed for comparison, invalid cache metadata, or an unprovable invalidation set also requires a safe rebuild rather than assumed reuse.

A downloaded mirror index remains an optional bootstrap aid. Establish its relationship to the requested source before treating it as an exact baseline. A heuristic covering the most recent number of commits does not establish that relationship. When mirror provenance cannot be established, a one-time local full index provides a reliable starting generation.

## Concurrent drivers and cleanup

Completed generations are immutable. Build replacements in temporary directories, validate both `PortIndex` and `PortIndex.quick`, and atomically publish the complete directory. A failed or interrupted refresh leaves the previous generation usable.

Coordinate writers for the same generation and updates to the master pointer. Keep locks scoped to those cache operations; unrelated candidate builds must not serialize behind one global index lock. Once a seed is copied into a private workspace, release any seed-copy protection before candidate indexing. Readers can continue to use completed generations while another generation is being built.

Collection must coordinate with generation publication and seed copying so it cannot remove files from an active reader. Retain the current master seed preferentially and bound older-generation retention. Staged copies belong to their operations; cache eviction does not invalidate recorded verification evidence. A cache miss is recoverable by regeneration.

Reuse existing file coordination where it fits. This cache does not require a new SQLite ledger or durable job lifecycle.

## Ownership and implementation sequence

Keep generation, identity, seed selection, staging, and retention in `internal/macports/portindex`. Git owns source snapshots and diffs. Application composition supplies a shared cache configuration. Discovery and verification consumers request an index for frozen inputs; they do not choose independent cache layouts. Workflow retains source/evidence policy.

Implement in this order:

1. Define generation identity, metadata, and coverage compatibility; consolidate consumers onto one cache root.
2. Replace the separate standalone/baseline/candidate storage paths with exact-generation lookup and explicit seed selection.
3. Add incremental advancement of the master seed and derive candidate generations from it.
4. Integrate atomic publication, concurrent use, and retention. Handle legacy cache paths and frozen provider settings explicitly; do not rewrite accepted job inputs merely to move disposable files.

No new production package or dependency is required by this design. Existing reusable indexing and validation mechanics should be retained where their contracts fit.

## Acceptance checks

- Two bumps from the same master reuse its baseline; the same candidate index serves discovery and staging.
- Advancing master across ordinary port changes uses incremental indexing. Changed subports, removals, and renames are reflected correctly.
- Shared-resource or indexing-environment changes force appropriate rebuilding.
- A contribution accepted against an older master keeps its exact source/index binding after the master pointer advances.
- Concurrent drivers never consume partial generations; canceled generation and concurrent collection preserve usable entries and private staged copies.
- Manual and historical sources can reuse compatible seeds without adopting another source tree.
- Measure cold generation, warm reuse, master advancement, candidate indexing, and retained disk space. Report full versus incremental passes explicitly.
