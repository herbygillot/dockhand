# Codex's architecture review, taken and coalesced into the roadmap

At the person's word, 2026-10-17 12:23Z, on the prioritization thread: "Take your time, look at the code, and meditate on what we might truly want from Codex's review. Whatever you think makes sense to address, go ahead and bring into the roadmap", then a coalescing run across the roadmap. Docs only.

Each finding of the [review](../reviews/2026-10-16-architecture-832f059.md) (of 832f059e, CX-1 to CX-12 in the roadmap) was checked against main at a268b6e5, eleven commits later, by reading the code; nothing was run. The test was what a user would notice, or what makes a planned step cheaper. An extraction no consumer asks for wasn't taken.

## Each finding

| | Finding | Holds at a268b6e5 | Taken |
| --- | --- | --- | --- |
| CX-1 | Ship policy has two owners | The gates were fixed for rc2 (e2dcb9b4). The drivers are still two, and one gate differs: `ship --mine` keeps an empty branch for a port dockhand can't edit, serve doesn't. | The empty branch, found after rc3 (Next's first paragraph). One gate for both when `branchstate` replaces `goOn` (v0.5.0 part 4, item 1). Not one driver: serve needs none of resume, follow, or commit binding. |
| CX-2 | Source identity misses `git.url` and the fetch kind | Yes | Part 1, item 2 (was the end of 5.1) |
| CX-3 | Corrections check fidelity three ways | Yes | Part 1, item 3 (was the end of step 3) |
| CX-4 | "Same revision" taken for "covers the request" | Fixed for rc2 (e2dcb9b4) | Done |
| CX-5 | The concrete port reader in engine; capabilities by type assertion | Yes: seven assertions on the port reader, most silent when missing, and the forge's `renamed()`. Only test fakes miss any. | The capabilities explicit, as C3 (part 2, item 1); the reader below engine with X1 (part 1, item 4), since `compose` builds it outside engine |
| CX-6 | Cached analysis has no evaluation context | Partly: change records are made natively and the plan doesn't read them yet; `ChangeRecord.Platform` is written but never read on reuse | The runtime kept on the change record as provenance, no migration, read when the plan first reads records (part 2, item 1). Assessments unchanged until a failure needs it. |
| CX-7 | Cargo targets read twice | Partly: create reads only the root package, so the review's lib-only member can't reach it; a lib-only root can. Found beside it: a nil `Programs` turns comparison's lib-only filter off, and an added program holds with removal's wording (epic-lore). | The two small ones in part 1, item 1. Cargo targets found once in `project` with Then's create work. |
| CX-8 | Discovery's probe built on portedit | Yes; no user-visible effect | Not now. When touched after step 2, if a read-only consumer still needs it. |
| CX-9 | go.mod read through depblock | Yes; the coupling is small. Beside it: `raiseGoToolchain` says it never refuses the bump, but a go.mod it can't read fails the update. | The refusal, as a notice, with part 1, item 3. Not the source-files reader. |
| CX-10 | Build reports by ordered callbacks | Yes, and handled safely: a late report makes inputs unknown. Beside it: the guest's `fact` turns a failed `port installed` into "read no other port", which reuse takes as complete. | The guest fix in part 1, item 1. One report value with part 6, where a new observation needs it. |
| CX-11 | GitHub clients with separate lifetimes | Yes, more of them than the review names. A renewal the Keychain didn't take lives in one client's memory; serve's identity watcher reads the spent login, and serve stops acting on GitHub until `setup github`. Conditional on a failed save; not reproduced. | One pending login per process in part 1, item 1; one runtime per process with X1 (part 1, item 4) |
| CX-12 | Tart image administration on the build provider | Yes, and the command package review put readiness there a day earlier. Beside it: `dockhand setup` ignores golden copies and offers a 60 GB download where a restore takes a minute. | The golden copy, found after rc3 (Next's first paragraph). Not the image service. |

The smaller follow-ups:
- **Downloads' transport:** true in form; every request goes through the same client today. One line, with X1's runtime.
- **distfetch's domain work:** `OwnArchives` moves with its one caller, the port reader, at X1. `LocalPatches` stays: it answers distfetch's own question, whether the direct downloader can fetch a port.
- **PR-status reducers:** two, which agree on every case read, and status and submit read one recorded state. They share their precedence first in part 4, item 1, where CI failures are classified.
- **File splits:** navigation only. CMake wording and records.go's results and executions go when touched (Taken when touched).

## The coalescing

v0.5.0 was nine items, 5.1 to 5.9, about 14 to 20 batches. That count was layer 1 alone, before X1, forgetting, and the split's verb packages moved into it, and before the review's items. It's six parts now, about 22 to 26 batches:
1. **Contracts and assembly:** the review's small fixes; CX-2; CX-3; X1 with `compose`, the host value, the port reader, and one GitHub runtime; forgetting an archived branch.
2. **One record:** the revision analysis, with C3; the ledger and decisions.
3. **The edits:** step 2; step 3.
4. **One verdict, then the split.**
5. **The interface.**
6. **New reach:** linked libraries, the one part that moves to Then if v0.5.0 runs long.

What moved, and why:
- **X1 before the record and the split** (was 5.8, after the split). X1 was first planned ahead of the split, with `compose`, the `setup` verb, and `cmdtest` its own work (the command package split's coalescing, 2026-10-15). Moving X1 and the split separately put the split first. Cut that way, each verb package would carry package-global seams (settings.go's `testPortReader` and `testActions`, author.go's `testPreparer` and `testForge`, and others), exported to a shared package for X1 to delete. C3's explicit capabilities are also filled in `compose`, once. It doesn't make CI faster: command's time is in Git processes, and its shards set that (the CI test times study, 2026-10-16).
- **CX-3 before step 3** (was the end of step 3). With one checked operation built first, step 3 changes one comparison, not three, and the patch-removal gap closes sooner.
- **Forgetting before the record** (was 5.9, last). Nothing depends on it, v0.5.0 migrates anyway, and field testing gets the whole cycle with it.

Later and Taken when touched were checked entry by entry. What was done is in [the roadmap's history](2026-10-16-roadmap-history.md#from-later-and-taken-when-touched-on-2026-10-17-done-or-now-in-v050), as the roadmap had it, as is what v0.5.0's parts now carry: the commit pin, CI failures classified, offline said as offline, what clean kept, `logs --phase`, the counted dependency the Portfile names, and the code-organization findings 9, 10, 11, 12, 26, and 44. Next's text before the change is in the same note, whole.

Found while checking, and wrong in the moved text: v0.4.0 part 2, item 3 said every "may this go on unattended?" question goes through one engine function, `goOn`. The command's ship doesn't call it; it reaches the same rules through `tidyBranch`. Part 4, item 1 makes it one gate.

The roadmap went from 74.8 KB to about 65 KB.
