<!-- The architecture thread's weighing of field testing's reflections, copied from the project's files on 2026-10-23, its owner's name given as "the person". The roadmap's Then coalesces it; where they differ, the roadmap's decides: see docs/activity/2026-10-23-field-reflections-coalesced.md. -->

# Field testing's reflections of 2026-10-23, weighed by architecture

Read against main aab5bafa on 2026-10-23. The person's ruling (field-testing thread, 20:10Z): the scope items stay out, except hold policies, which are weighed here with the rest. Each section says what already exists on main, what the proposal adds, where it belongs in the code, and a size. The order at the end is my ranking for prioritization to coalesce.

## Short version

- **Mostly already there or placed:** G is done on main but for the GitHub allowance (Later), D's classification exists in three of its four classes, C's license matching is already by identifier, F's CI cause is classified. The reflections' value is in A, B, E, the automatic baseline, and the hold policies.
- **A: not a "low confidence, write anyway" mode.** The check proves the build in dockhand's environments, not the Portfile: a checksum block for a distfile another platform fetches, a livecheck, a conditional block all build fine here and reach a pull request wrong. Instead, `update --draft` writes dockhand's best attempt as the person's hand edit, recorded and held as hand edits already are. Same effect for the person, no second meaning of "dockhand's edit".
- **One mechanism serves D, C, and E:** the old version built or installed beside the new, in the same environment. The baseline already exists as a command; run it by policy, and the configure-summary and install-name comparisons read from it.
- **Hold policies:** per-port overrides of the rules that exist, in dockhand's own configuration, typed as `holds.allow` is; one new rule kind, "after another port". No Portfile-adjacent metadata.
- **Two claims don't hold as stated:** the link scan reading only targets whose tests passed (an advisory failure keeps the outcome passed, guest.tcl:735, and links are read at :831), and the SPDX-labelled license false positive (fixed on main for v0.5.0). Each needs its check named if it recurs.

## A. Refusals resolved by hand: `--draft`, not low confidence

What exists: the probe (part 3.1) took 15 ports from refused to planned; host state (3.2) took 97 more; the cargo PortGroup checksums (32427a61), fresh's post-patch, and the libxkbcommon archive case are fixed or placed (L2a). The pattern will keep producing rows, as field testing says, and the roadmap's corpus item (a corpus of real Portfiles replaying the plan path) is where each new row goes first.

The proposal as written conflicts with the design in one place: "unverified edit, the check decides" makes the check the proof of an edit's fidelity, and a check proves only that the port builds where it ran. Strict fidelity (part 3.3) exists because an evaluation dockhand can't predict is a guess, whatever builds afterwards.

What gives the person the same path without that: `update <port> --draft`. Where the plan refuses, with --draft dockhand starts the branch, writes its best attempt (version, the checksums it fetched, the dependency blocks it could generate), and records the result as a hand edit with the refusal's reason, exactly as a Portfile edited in the worktree is recorded today: the change record says "hand edit: <reason>", tidy's strict record judges it, D4 holds it unattended, the pull request says it. Attended only; serve and bump never take it. New code: the flag and the handoff from the refusal to the hand-edit record; everything downstream exists. About one batch. Place it early: it is wishlist #2, and it costs little.

## B. Patch upkeep: a refresh with a prediction

What exists: patchcheck dry-runs every patch before the check and names the rejected ones; dropMergedPatches (portedit/patches.go) drops a patch the new source already holds, through the correction mechanism, which observes the Portfile evaluates as before but for it.

The helper is a second correction of the same shape, and it has an exact prediction: the tree after the regenerated patch applies cleanly must equal the tree after the old patch applies with fuzz and offset. That keeps "the check decides" honest, since the only risk left is the fuzzy placement, and the regenerated patch is in the pull request's diff for review. Owner: patchcheck grows Refresh (apply with fuzz, re-emit from the applied tree, keep the header comment, say which hunks landed with fuzz or in a generated file); portedit applies it as a correction. The generated-file flag is a path rule, pure. Side by side is view. About 1.5 batches, after v0.5.0.

## C. Configure-time drift: an observation beside links

What exists: the upstream comparison (source), and the guest's per-target observations (links, part 6). Nothing reads the configure phase.

The configure summary is a build-time observation, not a source one: the guest keeps the configure phase's summary lines from the port's log (autoconf's "checking for", CMake's "-- Found" and "-- Could NOT find", meson's "Dependency … found"), a per-target fact beside links; evidence diffs two summaries, pure; the ledger carries the finding as a notice, as links are. The diff needs the old version's summary in the same environment, which exists only where dockhand built it: the baseline (D) is that build. So this follows D's automation. About two batches, Then, after D.

License matching: already by identifier. project/license.go classifies with licensecheck's IDs and compares the ID sets (LicenseText.Same); libxkbcommon's SPDX labels were the coverage rule, fixed on main for v0.5.0. vault's licensor rename is a Terms false positive (the paragraphs no license covers): a small rule fix for implementation, with vault's file as the test.

## D. Failures that aren't the port's: run the baseline by policy

What exists, class by class: infrastructure (ExecutionInfrastructure, and the guest's own faults said at once); a dependency's failure (FailedDependency, "a dependency failed to install", and the standing acceptance of an environment failure, which is the Monterey libgcc15 case by its image identity); CI failures by cause (part 4.1). What a person still does by hand is `check --baseline` and then `submit --accept`.

The missing piece is small: a policy that runs the baseline the check already offers. After a check fails a target at install or test where master has the Portfile (BaselineCandidates), the check queues the baseline of those targets as its continuation; `check.baseline = offer|auto` in the configuration, auto for serve. The result is a notice with a Next, never an acceptance: "fails at master too, in the same phase: dockhand submit --accept <port>", since the ledger's rule is that dockhand never accepts a failure itself. About one batch.

Not now: the image's Xcode up front. The image's Xcode is a fact dockhand has; which Xcode gcc15 refuses is knowledge it doesn't, and packages.macports.org's missing darwin_21 archives are the tree-health signal the person ruled out. The standing acceptance covers the case after its first failure.

## E. Install names and dependents: the same shape as links

What exists: the guest reads what a passed target's binaries link (otool -L, with rpaths resolved); `update --revbump-dependents` and `--except` compute and bump the direct dependents.

The soname question is the mirror of links: what the target's own installed dylibs are called, their LC_ID_DYLIB install names and compatibility versions (otool -l), a per-target observation beside links. The comparison needs the old version's: the baseline gives it, or cheaper, the guest installs master's binary archive of the port before it builds the new one and reads its install names first, which is a `port -b install` the dependency isolation already does for dependencies. evidence compares, pure; the finding is a notice on the ledger naming the dependents and offering `--revbump-dependents`. About 1.5 batches. Place after D, with C.

The gap claimed for advisory tests doesn't hold as written: under an advisory policy the guest records the outcome passed (guest.tcl:735) and reads links for every passed outcome (:831). If Crystal's links were unread, its check will say why in the log ("what this build's binaries link wasn't read"); field testing should name the check.

## F. After submit

A merged pull request with new commits on its branch: the branch is merged, so `update` on the port starts a new branch (the rc5 A4 rule). What's missing is carrying the commits over: a Next on a merged branch whose Git branch has commits beyond the merge, `tidy --carry` (or a `start --from`), cherry-picking them onto a new branch from master. Half a batch, branchstate's verdict plus one engine operation.

The CI failure's error line: CI causes are classified (part 4.1), and the unclassified case says why the log said nothing. The buildlog reading that gives a local failure its first error line (D10) can read a CI job's log the same way, so status quotes the line for an unclassified failure. Half a batch, placed with the next CI work.

Stale cards in the project are the project's user interface, not dockhand's.

## G. Ergonomics: done but for one

Concurrent instances and schema: `status` lists the processes running, each with the schema it expects against the database's (engine/processes.go, part 1 item 7). Long runs: `check -d` starts its own drain. tidy's untracked files: a patch the Portfile names is committed (v0.5.0's last fixes). `path` outside the tree: X1. The GitHub allowance over 2,133 ports is in Later (conditional requests with ETags); the maintainer scale field testing met argues for Then. Nothing else to add.

## Hold policies, per port

What exists: `[holds]` with `majors` (hold or allow) and `allow` (rules by name, validated against the engine's HoldRules), applied where serve's candidates are held (servesubmit.go holding) and where a major is judged. MacPorts' own conventions already carry part of a maintainer's way: livecheck filters series and prereleases, which discovery honours.

The design: the same table, per port. `[holds.ports."harfbuzz"]` overrides `majors` and `allow` for one port, and adds two rule kinds: `follow-moves` (the `--follow-moves` flag as a standing choice, for unattended runs) and `after = ["harfbuzz-devel"]`, which holds the port's unattended submission while the named port's update is open or unsubmitted at the same version, the devel-first case. Owner: config owns the typed names (R3-16's rule), compose validates them as it validates `holds.allow`, the engine composes the port's effective holds in one place, and branchstate reads Held as it does. No per-maintainer scope: dockhand runs as one person, whose configuration this is. No Portfile-adjacent metadata, since dockhand can't add files to macports-ports. About one batch for the overrides and follow-moves, half more for `after`. It changes what serve does unattended, so the person's say on the two new rule names.

## Ranking, for prioritization

1. Hold policies per port (1.5 batches): unattended runs respect the maintainer's way; the person asked for it weighed now.
2. `update --draft` (1 batch): wishlist #2, reusing the hand-edit path.
3. The baseline by policy (1 batch): the mechanism C and E then read.
4. The patch refresh (1.5 batches).
5. Install names and dependents (1.5 batches), then the configure summary (2 batches), on the baseline.
6. The follow-up branch and the CI error line (1 batch together).
7. The GitHub allowance (from Later to Then).
Small fixes for implementation now: vault's license Terms false positive; and a check name from field testing for the Crystal links claim.
