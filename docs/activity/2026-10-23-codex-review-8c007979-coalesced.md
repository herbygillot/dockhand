# Codex's review of 8c007979, coalesced

On 2026-10-22 the person passed on Codex's architecture review of rc1's commit, 8c007979 ([review](../reviews/2026-10-22-architecture-8c007979.md), with its probes beside it). The architecture thread assessed it on 2026-10-23 at main cfa4530f, which is rc2's 1b3899e7 with the review's commit. All ten findings hold there, and its four probes fail as its log records. The prioritization thread then checked each finding against every section of the roadmap, as the coalescing rule asks (2026-10-19 18:45Z). Nothing was run here, and the change is docs only. The codes are C8-1 to C8-10, after the review's numbers.

## Where each goes

| | Finding | Weight | Placed |
| --- | --- | --- | --- |
| C8-1 | A command typed at `status --watch` runs in the process's directory and environment, without the watch's `--tree`, `--db`, or `--git` | P1, acts on the wrong object | Next, rc3, at the person's word |
| C8-2 | Link declarations pooled across environments hide an undeclared link | P2, a notice missed | Next, rides rc3 |
| C8-3 | A standing acceptance passes submit, but serve, `ship --passing`, attention, and `Next:` don't take it | P2, against the person's 2026-10-18 decision | Next, rc3, at the person's word |
| C8-4 | The archiver asks the forge PortGroup's repository for git.url's commit | P2, rare, a wrong tree compared where both hold the commit | Then, item 1, with C8-5 |
| C8-5 | A Git resolution failure loses its kind, so a blip is never looked at again | P2 | Then, item 1; its policy gap is an open decision |
| C8-6 | Status gathers while it renders | P2, organization | Then, item 6 |
| C8-7 | The assessment and `preparer()` in the engine; two ship drivers | P2, organization | Then, item 7; R3-5's residual when ship is touched |
| C8-8 | sourcecompare's callbacks carry assess's policy | P2 | When touched, with compare.go's split |
| C8-9 | Cargo programs inferred twice | P2 | Then's create item already (CX-7) |
| C8-10 | `Result.Files[0]` is positional | P3 | R3-40's row already |
| | Cleanup: architecture.md stale since X1 | doc | Next, with rc3 |
| | Cleanup: the Makefile's shard launcher | test speed | Maintenance, any time |
| | Cleanup: config provenance, compose's PATH, tart's provider.go | low | When touched |

## Where this differs from the assessment

- **The source as an observed value goes first in Then, not after its fourth item.** C8-4 can compare the wrong tree without saying so, and C8-5 leaves a network blip as a stale "couldn't be resolved". Both are correctness, about one batch together, and the trust rule's `review --check` reads sources after them.
- Everything else is placed as the assessment proposed.

## The size

rc3 is about two batches. Then grows from about 10.5 to about 14.5 batches: one for the source, one for status gathered once, and two for the assessment as a package.
