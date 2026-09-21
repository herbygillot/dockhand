# 2026-09-21: the index cache measured, the mirror as a bootstrap seed

## What the cache holds

The relay for Next items 6 and 7 said the cache is keyed per repository
registration and that this machine's seven full generations came mostly
from separate registrations re-indexing commits another registration had.
Measured, neither holds. The cache directory under `dockhand/indexes` is
keyed by indexing environment: the `portindex` executable's digest, the
MacPorts Base it loads, and the platform, as `openCache` computes it; the
two populated directories here are `/opt/local` and `/opt/macports-test`,
the same Base 2.12.6, with 181 and 26 generations, plus a legacy layout with
none. Generations are keyed by tree object, so every registration of a
repository already shares them. Of the 82 generations, 7 were full; the
first in the test prefix had no predecessor, and every other one followed
an edit under `_resources` since the previous generation, which the
incremental path rightly refuses:

| full pass at | shared files changed since the previous generation |
| --- | --- |
| 09-17 15:53 | xcode_versions.ini, R-1.0.tcl, java-1.0.tcl |
| 09-17 18:07 | app-1.0.tcl, app-1.1.tcl |
| 09-18 00:32 | xcode_versions.ini, R-1.0.tcl, app-1.0.tcl, app-1.1.tcl, java-1.0.tcl |
| 09-18 16:19 | java-1.0.tcl |
| 09-19 17:35 | archive_sites.tcl, java-1.0.tcl |
| 09-18 12:53 (test prefix) | java-1.0.tcl |

Every one of those trees exists in both the main clone and the opentofu
clone. Seeding across registrations would have saved none of them. The
item is replaced on the roadmap by the measured cause: a PortGroup edit
costs a full pass, and narrowing it to the ports that load the group needs
an inclusion map, because the PortIndex carries no `portgroups` field.

## Durations

`generation.json` now carries `duration_ms`, how long the indexer ran, so
the four-minute figure becomes a measurement per pass.

## The mirror as a bootstrap seed

For a cache with no usable generation, `portindex.Config.Mirror` enables
the MacPorts mirror's PortIndex as the seed of the first generation:

- The index is fetched with the bounded transport under the 128 MiB index
  limit; its `Last-Modified` less a margin, two hours by default, is the
  instant a master commit is looked for with `git rev-list --first-parent
  --before` from the target commit. The mirror's snapshot cannot predate
  that commit, so the paths changed from it to the target tree, through
  the existing `ChangedPaths` and incremental path, make the generation
  current for the target tree.
- The target commit must be no older than the index's `Last-Modified`;
  otherwise entries for ports changed after the target and before the
  snapshot would survive un-reindexed. A shared-resource change since the
  bracket, a missing `Last-Modified`, and no bracketing commit each fall
  back to the full pass with a verbose message saying why.
- The generation records `mirror`: the URL, `Last-Modified`, the
  bracketing commit, and the margin; it is never strict, since the mirror's
  indexer and Base are not the local ones and the executable digest cannot
  vouch for it. A strict candidate derives from it through the ordinary
  incremental path as from any base.
- Gating: `bump`, `verify`, the corrections, and dependent discovery pass a
  mirror to the index configuration; `assess` and `outdated` pass none and
  make no network request for the index. `survey.Open` no longer takes the
  HTTP client it never used, so the offline guarantee is in the signature.

Live, against a cold cache with `DOCKHAND_INDEX_CACHE` pointed at an empty
directory, `bump flyctl --dry-run` took 21 seconds end to end: "Seeding the
PortIndex from the mirror index of 2026-09-21T14:25:08Z, re-indexing 13
paths changed since master 4312c587140b", 3.0 seconds of indexing, and a
generation whose record carries the provenance and `strict: false`.

## A defect found on the way

The local indexer is invoked with `-p darwin_25_arm64`, while the mirror's
index is `darwin_25_arm` and MacPorts reports `os.arch` as `arm` on Apple
silicon. `portindex -p` sets `os_arch` verbatim from the flag's third
segment (`/opt/local/bin/portindex`, line 350 in Base 2.12.6), so the
local index evaluates every `${os.arch}` condition with a value no Portfile
compares against, and mirror-seeded entries, evaluated with `arm`, differ
from locally indexed ones for such ports. That predates this work; the fix
is to pass the kernel architecture `DefaultMirrorURL` already maps to, and
it changes the cache identity of every local generation, so it is its own
change.

## Evidence

- `TestMirrorBootstrapSeedsAColdCacheWithinItsBracket` builds a mirror index
  from an older commit, serves it with a `Last-Modified` that brackets that
  commit, and checks the seeded generation's entries, provenance, non-strict
  record, duration, and removed seed; then a strict candidate deriving from
  it, a target older than the index, a shared-resource change since the
  bracket, and a cache without the mirror, which touches no network.
