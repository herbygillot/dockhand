# 2026-09-27: what an update couldn't check holds a submission nobody reviews

The person decided D4: what an update couldn't check holds `bump`'s and serve's submissions for a person's look, as a failed search for other pull requests already does. The code-organization review's finding 32 had found the gap.
- The upstream comparison, which holds an update whose license, build files, or declared dependencies changed, never runs for a port with `go.vendors` or `cargo.crates`: their updates keep no old archives.
- It reported "the versions have 0 and 1 distfiles, so they can't be paired".
- The only holds were a change's.
- So both submitted every such update with no comparison at all, silently.

**The first part, done here.** A comparison that couldn't be made holds.
- `UpstreamComparison.Holds` lists what holds an update: each change that does, and, where the archives couldn't be compared, why.
  - Couldn't be compared means they couldn't be fetched, couldn't be paired, weren't kept, or couldn't be read.
  - `Held` is whether there is any.
- `upstreamHolds` reads them from the branch's recorded edits, so `serve`, `bump`, and the attention list all see them.
- A port with no archives, such as one fetched with Git, has no comparison at all, and isn't held for one.
- `update --json`'s `upstream.held` now says the same.

For a `go.vendors` or `cargo.crates` port this holds every unattended update, until such an update keeps its old archives to compare. That is the next part, with the other two things an update can't check:
- a Go toolchain minimum dockhand couldn't rewrite;
- patches it left unchecked.

Today each of those is only a progress line nobody sees (finding 31). They become typed facts that travel with the update, and hold.

**Tests.**
- `TestServeHoldsAnUpdateWhoseArchivesCouldNotBeCompared`: the attention list and serve's candidate both give the reason, and nothing is submitted.
- `TestAnUpstreamComparisonHoldsForWhatItCouldNotCheck`.
- The engine tests' fake preparer can now keep only the new version's archive, as a vendored port's update does.

**Docs.**
- Design §11's guardrail.
- The usage guide's `serve`, `bump`, and `update --json` lines.
- `bump`'s help.
- The roadmap records D4 and D5 as decided. D5: tidy, rebase, and restore stay in the engine, and mechanisms they share can live in `history`.
