# 2026-09-28: reusing some targets, building the rest

Roadmap item 6, decision 28. Until now an environment reused earlier builds only when every target would read what its earlier build read. Otherwise it built them all. Now each target that would read the same reuses its build, and the rest build.

**The rule** (`reuse.Choose`).
- A target reuses its newest earlier build whose result stands, by the check's test policy, and whose inputs are what it would read now (`reuse.Current`, as before).
- The rest build, and so does any target one of them needs. A reused build isn't in the guest to be installed. MacPorts would install a dependent's copy of it from upstream's archive, of the port as master has it, or build it unrecorded as a dependency.
- A target needs what the plan says it does, and what was active as its newest earlier build ran. That second part catches a target it reaches through ports the branch doesn't change, which the plan doesn't know.
- A taken target takes what it needs in turn.

In the fixture, a library, a tool that links it, and a viewer that links neither:
- a change to the viewer builds the viewer alone;
- a change to the tool builds the tool and the library, and reuses the viewer.

**Where it lives.** The decision is `internal/reuse`'s, as item 4 planned for reuse leaving the engine. The engine reads what it needs: the candidates from the store, the revision's trees from Git, and the environment's identity, which is now read once for each attempt.

**What's recorded.**
- When every target reuses, nothing is built, as before.
- Otherwise one execution holds both: the reused results, each naming the execution that built it, recorded before the provider starts, and the results of what the provider builds. The provider is given only what builds.
- A retry after an infrastructure failure keeps the reused results, as it keeps any finished target's.

**Two reads follow the result, not the execution.**
- `Reusable` offered only results from executions that reused nothing. An execution can now hold reused results beside built ones, so it filters by each result's `reused_from`: a reuse is never offered as a build.
- The pull request's Tested on shows, for each result, the run that built it. So a run that built some targets shows beside the runs it reused. Before, only a run that reused everything was resolved to its builders.

**What people see.** A check says "libharbor, harbor-cli would build as in check-1, and reuse those results; the rest build". `logs` shows each reused result "as built by run tart_…".

**Known gaps.**
- A target with no earlier build needs only what the plan says. Planning orders targets by what they declare, so a dependency through a port the branch doesn't change is ordered by chance there too. Planning's move in item 6 can take such dependencies from the port index.
- On a retry, a target an earlier attempt finished isn't in the new guest either. Its dependents install it as they would a reused one, unrecorded. Kept archives, next, fix both: the guest installs such a target from dockhand's own archive of it (decision 44: "retry consumes only complete applicable results with their archives available").

**Tests.** Each fails with its part undone:
- `TestTargetsReuseWhatStandsAndBuildWhatTheBuiltOnesNeed`: `Choose`'s cases, one at a time.
- `TestTheTargetsThatChangedBuildAndTheRestAreReused`: five checks of the fixture. It covers what the provider is given, which build each reused result names, and what Tested on shows. It fails without the taking of what a target needs, the needs from its last build, the `Reusable` filter, or Tested on's resolution.
