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
