# PortIndex consolidation

Implements the [PortIndex storage design](../portindex.md), roadmap item 2 after the new-user exercise. The exercise had shown the same full index generated twice: once for discovery in the user cache and once for Tart staging under the artifact directory.

## Change

`macports/portindex` now keeps one cache root for every consumer, with one environment per indexer digest, MacPorts Base runtime, and platform. `ResolveTool` probes the Base version through the launcher's Tcl interpreter, so upgrading MacPorts without changing the `portindex` script still separates environments. Each environment stores immutable generations under `generations/<tree>` with `PortIndex`, `PortIndex.quick`, and `generation.json` recording the seed, changed-path count, pass type, and strictness. Builds happen in temporary directories and are published by rename; readers hold the environment lock shared and builders of one tree serialize on that tree's lock, so unrelated builds never queue behind one another and collection cannot remove an entry in use.

`Stage` looks up the exact generation first. Otherwise it selects a seed: the recorded base for a candidate, else the environment's `latest` pointer, else recently used generations, skipping any whose tree is unknown to the repository or whose diff touches `_resources`. A contribution first ensures its base generation, then derives the candidate strictly from it; base-like trees advance the pointer and candidates never do. A strict request never reuses a generation built without `-e`, so a partial discovery index cannot hide a changed port's failure from verification; the strict rebuild replaces it. Once installed into a staged root, unchanged files are not copied again.

Tart, dependent discovery, assessment, `outdated`, and name lookup pass the same cache root, supplied by application composition as `tart.Provider.IndexCache` and `surveyIndex`. The frozen provider configuration is unchanged; the recorded mirror URL stays as provenance but staging no longer downloads a mirror index, since a mirrored index cannot prove which source tree it describes. The legacy `indexes` cache under the artifact directory is only collected. `Collect` handles both layouts, keeps the `latest` seed regardless of age, and removes leftover build directories.

No new package or dependency. `standalone.go` was folded into the shared lookup.

## Checks

- `go test ./... -count=1`, `go vet ./...`, and `go test -race` on `portindex`, `verify/tart`, `verify/staging`, and `app`: passed.
- New `portindex` tests cover one generation shared across consumers with the environment description written, a candidate deriving from its base and later master advancing incrementally from the retained seed, strict requests rebuilding rather than reusing partial generations, tree-diff reuse with removals and `_resources` invalidation, three concurrent stagers producing one full pass, and retention across busy, current, and legacy environments.
- The Tart mirror-seeding test became a shared-cache derivation test; the legacy artifact cache is asserted unwritten.

## Real-tree measurements

All against `~/Source/macports-ports` with `/opt/local/bin/portindex` (MacPorts 2.12.6, darwin 25 arm64), starting from an empty environment because the layout key is new.

| Step | Before (new-user exercise) | After |
| --- | --- | --- |
| Cold full pass | 3m40s (discovery) + 3m35s (Tart staging) | 3m55s, once |
| `bump-revision deno --wait`, cold cache, fresh database | 9m58s | 6m38s |
| Candidate index inside the verification attempt | 6s incremental after the second full pass | 4s incremental from the cached base |
| `outdated deno` on local HEAD after the bump | full pass | 3s incremental from the master generation (14 changed paths) |
| `bump-revision deno --diff` | index refresh | cached master generation; 15.6s total |
| `assess deno --version 2.9.7` | 10.9s | 10.7s, cached |
| Cache size | 178M Tart + 78M discovery (existing caches before) | 129M shared, including test fixtures |

The remaining first-run cost in a bump is hashing the Tart image, reported separately. A warm `outdated deno` still takes about 85s with the index cached, so index generation was never the dominant cost of that command; the remainder is materialization and evaluation and is a separate finding.

## Follow-ups

- Package tests in `app` and `cli` stage indexes through `os.UserCacheDir`, so `go test` writes fixture generations into the user's real cache. That predates this change; the fixtures are tiny and collected by `gc`, but tests should be pointed at a temporary cache root.
- Investigate the 85s of `outdated` that is not index work.
