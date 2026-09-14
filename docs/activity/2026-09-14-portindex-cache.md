# Frozen-source PortIndex cache

## Change

Tart verification no longer runs a full `portindex -f` inside every disposable VM. Before staging input, the provider generates a platform-specific index from the immutable Git tree using the local MacPorts installation selected by `--prefix` / `MACPORTS_PREFIX`, or `portindex` on `PATH`.

On a cold cache, Dockhand follows the approach used by MacPorts' MPBB bootstrap: it downloads `PortIndex_darwin_${OS_MAJOR}_${OS_ARCH}/PortIndex` from the MacPorts mirror, marks port directories changed in the preceding ten base commits for reconsideration, and runs incremental `portindex`. A failed download, insufficient base history, or failed reconciliation falls back to a complete local index. The mirror removes the ordinary cold-start full-tree evaluation while the fallback preserves operation without it.

The indexer receives an isolated `PORTSRC` configuration whose source is the materialized tree and whose registry and variants file are temporary. This ensures PortGroups come from the accepted source rather than the user's installed ports source and keeps indexing out of the user's MacPorts registry.

For a prepared contribution, Dockhand retains the reconciled index for the immutable base tree under the Tart artifact directory. It derives each candidate index incrementally by comparing Git trees and making `portindex` reconsider every port directory whose Portfile or supporting files changed. A change under `_resources` forces a complete candidate index because a shared PortGroup may alter arbitrary port metadata. Candidate indexes are temporary, so individual bumps do not each retain another full copy. Concurrent processes serialize construction for the same platform, mirror, and indexer profile through the provider's external-resource lock mechanism.

The accepted Tart configuration records the resolved `portindex` path, executable digest, and mirror URL. Execution refuses a changed tool rather than silently producing different build input after acceptance. The guest requires the staged full and quick indexes and proceeds directly to target evaluation and build phases.

## Limits

MacPorts mirror indexes do not carry their generating Git commit. Dockhand uses the same ten-commit reconciliation window as MPBB, so an unusually stale mirror can still supply stale entries outside that window. A shared PortGroup change requires a full pass. Retention policy for obsolete base-index cache entries remains future work.

## Validation

Tests cover mirror URL and download handling, mirror-seeded base reconciliation, cache reuse, concurrent construction, incremental candidate generation, non-retention of candidate indexes, staged index presence, and full invalidation for PortGroup changes. A real MacPorts integration test proves that both full and incremental indexes resolve a PortGroup from the frozen source and preserve the changed candidate revision. The full Go suite, vet, build, and whitespace checks passed during implementation. All cache and test code was authored for v2; no new package or dependency was introduced.
