# 2026-09-25: the implementation review's four findings

The [2026-09-25 implementation review](../reviews/2026-09-25-v3-implementation-review.md)
found four gaps in v3's check, tidy, and submit guarantees. Its probes
([patch](../reviews/2026-09-25-v3-regression-probes.patch)) reproduced
each on the Mac, and each contradicted Design v3's own wording. They are
fixed here, before the Tart provider, whose acceptance journeys run
through all four.

## 1. A narrowed check never shrinks what submit requires

`check --only jq` on a branch that also changed libharbor let `submit`
plan a ready pull request: the evidence was the last run's targets alone,
and `--only` had narrowed `Plan.Targets` in place, keeping no record of
what it left out (§7: "A narrowed check never quietly shrinks what
`submit` requires").

- **The plan keeps its changed scope.** `model.Plan.Omitted` records the
  changed targets `--only` left out, and `check` prints them: "Left out
  libharbor, by --only; submit still needs them checked". Plans are JSON,
  so no schema change.
- **Evidence is the tree's, not one run's.** `treeEvidence` starts from
  the newest finished check of the files and judges it against everything
  its plan required, the omitted targets included. It fills what that
  check didn't build from earlier checks of the same files, newest first,
  environment by environment, since a result holds for its tree. So a
  narrowed check after a full one keeps the full one's results, instead
  of superseding them. `Evidence.Earlier` names the checks it drew on,
  and the pull request says so: "Checked by dockhand check-3, with
  results from check-2 for the same files."
- **What nothing built blocks.** A target no check built everywhere it is
  required is `Unchecked`. A changed one blocks submission ("no check of
  these files built it; dockhand check builds it, or share the branch as
  a draft"), can't be `--accept`ed, since there's no failure to accept,
  and shows as "· not run" in the pull request's table.
- **One predicate.** `status` uses the same tree evidence.
  `PassingBranches`, which `submit --passing` and serve both read, now
  also requires that nothing failed or went unbuilt. The attention list
  says "check-2 passed, but no check of these files built libharbor".

`TestANarrowedCheckNeverShrinksWhatSubmitRequires` is the review's probe,
extended:

- **The narrowed check alone.** Submission is blocked, `--accept` is
  refused, the pull request shows the row, and `PassingBranches` doesn't
  count the branch.
- **Full check, then narrowed.** Submission is clear, and the evidence
  cites the full check.

`TestCheckPlanNamesWhatOnlyLeftOut` covers the plan's line, and
`TestPlanValidation` refuses a target both planned and omitted.

## 2. Changed shared code is substantive

A port whose own diff was `revision 0` to `revision 1` was classed
revision-only even when a PortGroup it loads had changed on the branch,
so a failure the PortGroup caused could be `--accept`ed as "cause not
established" (§3: revision-only "shared code included").

`targetKind` now also asks `loadsChangedSharedCode`, which settles the
question from the source and answers yes to anything it can't settle:

- **Outside `port1.0/group`.** A change elsewhere under `_resources` reaches
  every port, since Base itself reads those files: the compiler lists and
  the mirror sites.
- **A changed PortGroup** reaches the ports that load it, directly or
  through another PortGroup, following the `PortGroup` lines of the
  Portfile and of each group it loads, at the branch's tree.
- **Unknowns.** A `PortGroup` line that doesn't spell its name and version
  literally, a group file the tree lacks, or a file that names
  `_resources` itself counts as loading the change.

A blanket "any `_resources` change" rule was tried first. It broke two
existing tests that rightly keep a revision bump revision-only when the
changed PortGroup is one the port never loads, so its dependents aren't
in question for `impact`.

`TestChangedSharedCodeIsSubstantive` covers six cases:

- a revision bump alone;
- a PortGroup the port doesn't load;
- one it loads, which is the review's probe;
- one it loads through another group;
- the compiler lists;
- a PortGroup line the source doesn't spell literally.

Revision-only, and so acceptable, only in the first two.

## 3. A tidy checkpoint keeps the index

A checkpoint recorded only the old and new heads. Tidy reset the index to
the new head, and `restore` reset it to the old one. So an index holding a
staged version that was in neither head nor the working files lost it for
good, though §8 has the checkpoint save "any captured index or working
state".

- **Recorded and kept.** Tidy writes the index's tree before anything
  moves, and records it as the checkpoint's `Index` in a new
  `index_tree` column (schema 9). It keeps the tree reachable under
  `refs/dockhand/checkpoints/<name>-index`, a commit of it on the old
  head. `clean` removes that ref with the checkpoint's own.
- **Restored, after checking for newer work.** `restore` refuses if
  anything was staged since the tidy (the index is no longer the new
  head's tree), and otherwise puts the recorded index back. A checkpoint
  without one, such as a rebase's, which only runs with nothing
  uncommitted, resets the index as before.
- **Sparse-safe.** `git.SetIndex` restores the index entry by entry, from
  a `diff-tree` of the current index against the recorded one, through
  `update-index --index-info`. A wholesale `read-tree` drops the
  skip-worktree bits of a sparse worktree, so every port outside it read
  as deleted. The existing restore test caught that, and a throwaway
  repository confirmed it before the fix.
- **Saved plans.** A tidy plan records the index's tree when it is made,
  including in a saved plan's optional `index` field. Applying it, or
  loading a saved one, refuses if something was staged since, as it
  already refused new commits or edited files.

Tests:

- `TestRestorePutsTheStagedVersionBack` is the review's probe, extended:
  the ref keeps the index, restore refuses over newer staged work, then
  restores the staged version with the working files untouched.
- `TestSetIndexRestoresARecordedIndexInASparseCheckout`.
- `TestASavedPlanAppliesUntilTheBranchMoves` now also stages something
  after the plan.

## 4. Each platform keeps its own dependencies

The planner evaluated each environment, but once it had seen a port it
skipped that port's dependencies in later environments. Every
environment inherited the first one's graph. With arm64 first,
`--only harbor-viewer` lost the changed libharbor that x86_64's
harbor-viewer links, and reversing the environments changed the plan.

- **Kept per platform.** The plan records `Dependencies`, each
  environment's graph among its targets, in the environments' order, when
  it has more than one environment. `Plan.DependsOnIn` answers for one
  environment, falling back to `DependsOn` for plans made before.
- **The union for the whole plan.** A target's `DependsOn` stays the union
  across platforms. That is right for what `--only` must add back, since
  the plan's targets are the same in every guest, and for the one build
  order, which then satisfies each platform's.
- **Each platform's own for a guest.** The runner blocks a target only on
  what it needs on its own platform. The jobs providers receive carry that
  platform's dependencies, not the union. The baseline plan carries them
  over.
- **Cycles only across platforms.** A cycle that exists only in the union,
  such as A needing B on arm64 and B needing A on x86_64, leaves the plan
  unresolved with its own message, "dependency cycle across platforms,
  which no one platform has … check each platform on its own (--on)",
  rather than guessing an order.

`TestEachPlatformKeepsItsOwnDependencies` covers:

- **Order-independent.** `--only harbor-viewer` keeps libharbor with
  either environment first.
- **Blocking.** libharbor failing leaves arm64's harbor-viewer built and
  x86_64's blocked.
- **Jobs.** Each job carries its own platform's dependencies.
- **Cycles.** The cross-platform cycle is refused.

`TestPlanValidation` refuses dependencies for the wrong number of
environments, or one outside a target's `DependsOn`.

## Validation

The review's four probes, run against the tree after the fixes, all
pass. They are kept as the regression tests above, in the tree's own
words. `make test`, `make vet`, `make fmt-check`, `make lint` (0
issues), and `make vendor-check` pass on the Mac.
