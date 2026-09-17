# Version and port resolution: consolidation review

Written after the git-devel exercise, whose four fixes landed in four packages for one port. The machinery is sound: ordering is MacPorts's own `vercmp` through the native evaluator, tag patterns round-trip, explicit selections freeze a commit, and explicit versus automatic is a policy split. What accumulated is seams. This review names them and the moves that close them; the activity reports record each move as it lands.

## Findings

1. **One concept, five homes.** Spelling validation in `upstream`, tag patterns in `macports/source`, stability in `upstream/releasever`, evaluation in `portedit`, ordering in `eval`.
2. **Two selection paths that must agree.** Automatic selection in `DiscoverPort`, explicit selection in `Resolve` and `MatchRelease`, and a third copy of automatic selection for HTTP listings in `http.go`. Every policy change lands twice or three times.
3. **Three interpretations of one convention.** `macports/source` exposes `Interpret`, `Discover`, and `ForEditing` over the same PortGroup options at different strictness.
4. **`record.Release` mixes identity with decision.** The frozen fact (forge, tag, commit, listing) shares a struct with how it was chosen (requested spelling, current version, no-update) and how it classifies (stability).
5. **The read classifier is becoming a static analyzer.** Two dimensions, benign sinks, taint through `set` and loop variables, hook containers. The evaluator already records every read with its frames.

## Moves

1. `macports/version`: a leaf owning `Validate`, `TagPattern`, and `Classify` with its `Stability` vocabulary. `upstream/releasever` folds into it; `upstream` and `macports/source` use it.
2. One candidate pipeline in `upstream`: enumerate candidates from any catalog, map them to Portfile versions in one batch, then select with a policy that is either a requested spelling or the automatic rule. Listings, tags, and releases feed the same pipeline.
3. One `Interpret(port, purpose)` in `macports/source`.
4. `record.Release` embeds a `Selection` (requested, current, no-update, stability); JSON shape is unchanged because embedded fields flatten.
5. Classify observed reads by the command that consumed them, from the evaluator's frames, with the same benign vocabulary; keep the static scan for Darwin boundaries in branches that did not run.

No behavior changes are intended. The existing suites and this week's live controls (Terraform, Deno, goreleaser, Codex, abendrot, bun, warzone2100, fldigi, mrustc, git-devel) are the acceptance set.
