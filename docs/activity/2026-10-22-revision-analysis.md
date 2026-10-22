# One revision analysis (C1)

Roadmap part 2, item 1 (Codex's review of 386ac2cc, finding 4). The
change record, the assessments, and the plan each derived what a
revision changes for themselves: the base commit's tree, the paths
between it and the revision's tree, and the ports those reach.

`engine/revision.go` has `RevisionAnalysis`, `{Base, BaseTree, Tree,
Paths, Scope}` with `Sources()`, the shape the 2026-09-27 review's
finding 26 asked for rather than a struct threaded through every call.
`Engine.analyze(base, tree)` makes one once for each tree and base in the
engine's life, as both are Git objects, which don't change, and hands each
reader a copy. `revisionChanges`, `revisionAssessments`, and `PlanCheck`
read it.

Status, the baseline's narrowing (`readsChanged`), submit's plan, and
diff read it too, where each made the same prologue.

Not absorbed: `movedSources` and status's source-moved check read the
network, whether a Git source moved since the check was planned, which no
analysis of a revision's files can hold, and a cached one would go stale;
they stay where they are, beside the evidence they read.

## The comparison predicates leave words.go (R3-23)

`SourceUnchanged`, `NothingCompared`, the `notCompared` and `sourceUnchanged` coverage policies they read, and `ComparisonLines` with `ComparisonLine` moved from words.go to assessment.go, beside the comparison that writes those policies. They're judgments about what a comparison found, and `ComparisonLine.Hold` is one, so they don't belong with the wording. `Grouped` and `UnderUpstream` stay in words.go, because they shape words.

## Update's branch selection moves to branches.go, and two of its seams are taken

update.go held the code that picks and names branches, which isn't the update's: `worktree`, `openWorktree`, `BranchesChanging`, `PortBranch(es)`, `branchChanges`, `branchTree`, `ChangesHere`, `workingEdits`, `renameForVersion`, `FreeName`, `NameFor`, `nameFree`, `refSafe` and `shortID`. All of them moved unchanged to branches.go. `expandFor` stays, because it widens a sparse cone for the files an edit writes.

`update` lost two seams (code-organization finding 44):
- `followMoves` covers both kinds of following a renamed GitHub repository: inside a version edit, and as an edit that is the move alone. It reports the second.
- `editFindings` covers the primed assessment and the change record the edit's own work makes, where the branch was at its base.

The recording transaction stays in `update`.

## The single-file distfile: nothing left to absorb

The roadmap's "absorbs … the single-file distfile" was written on 2026-10-13. Two days later, b495055a made that judgment once: `macports.PortInfo.Extracts` says which distfiles MacPorts extracts, and `engine.readDistfile` reads each distfile the way MacPorts treats it. Both update's `readPairs` and the assessment's `readPlans` use it. No second judgment remains to fold into the analysis, so this bullet changes no code.
