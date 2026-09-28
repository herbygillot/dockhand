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

## The rest, the same day

**A Go or Cargo port's update keeps its old archives.** It already fetched the current version's source archive to check its dependency list against, into a scratch store it deleted. When the update keeps archives to compare, it now fetches all of the current version's archives, the port's own source without the dependency list, into the kept store. The comparison then pairs them with the new version's. Each version's archive is still fetched once.
- The Git-pinned crates a Cargo update fetches for their checksums are the result's `Crates`, apart from its `Downloads`: dependencies, with no counterpart in the current version to compare.
- They still count among the distfiles the update names.
- So a `go.vendors` or `cargo.crates` update is compared, and holds only for what the comparison finds, as any other does.

**A Go toolchain minimum left below go.mod's holds.** This covers the two cases where dockhand leaves `go.toolchain_min` below what a module-mode port's go.mod requires:
- the port declares none, and declaring one is the maintainer's call;
- the declaration isn't one literal dockhand can raise.

Each was only a progress line nobody saw. It is now the result's `GoToolchain`, and the update's upstream comparison carries it as a finding that holds (`toolchain`, on `go.mod`), since the builder's Go is new enough for the build to pass. `update` shows it with a `!`, and the attention list, `serve`, and `bump` hold on it.

**Unchecked patches are shown, not held.** This is a correction to D4 as first proposed.
- A Git-fetched port's patches can't be checked before the build, since its source isn't extracted here, and were only a progress line.
- They are now recorded as unchecked, shown with a `·`, and listed in `update --json`'s `unchecked_patches`.
- They don't hold: MacPorts' patch step applies every patch and fails on one that doesn't apply, so a passing check has proven them.
- The person is told, and can have them hold anyway.

**The result they travel through.** `preparation.Result` now embeds `portedit.Result` (the review's finding 19, first half), so the two new facts cross into the engine with no copy to forget. `preparation.GoToolchain` is the engine's name for the fact, since the engine doesn't import the editor.

**Tests.**
- `TestGoDependencyPreparation/kept`: the old archive is kept beside the new, each fetched once. It fails without the fix.
- `TestCargoGitArchivesUseEvaluatedPortGroupLocations`: the Git crate is apart.
- `TestToolchainMinIsLeftAloneWhenNotRaisable` gains a declaration that isn't a literal, and checks the fact in each case.
- `TestAGitFetchedPortsPatchesAreRecordedUnchecked`.
- `TestServeHoldsAnUpdateWhoseGoToolchainNeedsALook`.
- The MacPorts integration tests ran against `/opt/local/bin/port-tclsh`, evaluating only.

