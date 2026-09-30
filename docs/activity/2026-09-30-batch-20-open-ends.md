# 2026-09-30: batch 20's three open ends (batch 21)

[Batch 20](2026-09-30-git-source-bound-to-build.md) bound a Git-fetched port's source to its build, and item 9's step 3 made a tag moved after the check a concern at submission (`movedSources`). The roadmap's batch 21 took the three things batch 20 left open:
- preparation's release commit wasn't compared with the plan's, so an update prepared at a tag's old commit was checked at its new one, and nothing said so;
- evidence filled in from an earlier check didn't follow a Git-fetched dependency's commit to what was built against it, as reuse does;
- a target `--only` left out had no expected commit, so an earlier result of it filled in whatever it had fetched.

## One rule for what was built against another source

`reuse.Choose` rebuilt a target whose earlier build had active an archive of a Git-fetched target that no build of the commit expected now made, and whatever needed one of those in turn. That rule is now `reuse.AgainstOtherSources`, with one owner, and both reuse and evidence use it:
- **Reuse** is as it was: `Choose` drops from what it reuses the targets the rule names, then takes what the rest need, as before.
- **Evidence** takes an earlier check's result only where the rule doesn't name it. `engine.Counts`, the one rule for whether a result stands, gains the case: it wasn't built against another source of a Git-fetched target than the newest check expects, directly or through another target's build (`other`). `readSources`, which replaces `readFetched`, reads the earlier check's inputs and asks the rule, one environment at a time, over that check's own builds. Within one check a target's build read that check's result of what it needs: the build made there, or the earlier build it reused, installed from its kept archive, whose result carries that build's archive and inputs. So the builds the rule looks for a Git-fetched target's archive among are the same check's.
- Passed and failed results are judged; a failure against the old commit's build says nothing of the new one. A blocked result built nothing and read nothing, so the rule has nothing of it to judge, and it stands as it did. Judging it would have turned every blocked result that names a Git-fetched target it needs into "not checked" whenever it's filled in, though nothing moved, since a blocked result records no active ports.
- A Git-fetched target the newest plan expects, which the earlier check has no result of, is among the rule's targets with no build: a build that had it active all the same, reaching it through a port the branch doesn't change while `--only` left it out, had an archive dockhand can't place (MacPorts' own, of master's version), and doesn't stand. Reuse has every target of the environment among its targets, so this is the same reach.
- Nothing is read where the newest check fetches nothing with Git, so evidence costs what it did for every other branch.

A consequence to know: a build whose provider didn't say which ports were active can't be established to have had the commit expected, since the rule finds the dependency's build by the archive that was active. Reuse never reused such a build, its inputs being incomplete. Now an earlier check's result of a target that needs a Git-fetched one doesn't stand either, where its provider didn't say what was active: GitHub's builds, and a command provider that reports what a fetch checked out but not the active ports. Tart reports both, for passed and failed builds alike, so for Tart the rule is exact. Before, such a result filled in regardless.

## A target left out expects its commit

`planning.Decide` builds each environment's plan from the targets `--only` keeps, and `EnvironmentPlan` recorded a Git source only for what it orders, so a left-out target had none, and nothing resolved its tag. What that cost:
- **Evidence:** a left-out target filled in from an earlier check whatever its build fetched, since the newest plan expected nothing of it, and a target built against it did too. A tag moved since the earlier check, followed by `check --only` of something else, left a branch's evidence standing for a commit it no longer fetches.
- **Submission:** `movedSources` walked the plan's Git sources, so a left-out target whose tag moved after the check wasn't said to have moved, though its result, filled in, was what the pull request rested on.
- **Reuse and the guest:** nothing. A left-out target is in no environment's order, so it's never sent to a provider, never reused, and never installed from a kept archive: `--only` adds back the changed prerequisites a kept target needs, so none it needs is left out.

So a left-out target now has one:
- `planning.OmittedSources` gives each environment's plan the source of each target `--only` left out that is fetched with Git there and not ruled out there, as the plan's own targets have. `Decide` calls it for each environment's plan.
- `resolveGitSources` resolves every source in `EnvironmentPlan.Git`, not only those of targets in its order: each repository and ref once, with a minute's timeout, as before. A narrowed check of a branch with a left-out Git-fetched port now reads that repository's refs; its tag is read when the check is planned or not at all, since evidence is judged from the store alone.
- `Plan.Validate` accepts a source for a target the plan omits, where that environment doesn't exclude it, and still refuses one for a target it neither builds nor omits.
- `EnvironmentPlan.Git` says so. `check --plan` and its JSON still show the sources of what a check builds only; a left-out target's is used by evidence and submission, not by the check.

A left-out target whose tag can't be resolved now has an expected commit of none, so an earlier result of it doesn't stand until a check resolves it, as a built one's doesn't.

## The update's release against the plan

`update` records the release it chose on its edit (`Edit.Release`), with the commit its forge said the tag named. The check resolves the tag again when it's planned. Where the two differ, the check built another commit than the one the update chose.
- **`model.Release.Names`** says whether a Git fetch's source is a release's: its `git.branch` the release's tag, or its commit where the Portfile pins one, and its `git.url` the release's repository, as the github and gitlab PortGroups write it, the repository's address with `.git`, in any case. An archive's release, or one found without a repository, names none.
- **`engine.preparedSources`** matches the newest update of each planned Git-fetched target's port whose release names the planned source, and where the release's commit isn't the plan's, it's a concern, `release-moved`, of the same family as `movedSources`' `source-moved` and in its voice: "libharbor's git.branch v4 named aaaaaaa when its update chose it, and bbbbbbb when check-7 planned it: the check built another source than the update chose". A release or plan with no commit says nothing. An update since edited by hand to another tag, or another repository, isn't matched.
- It goes in `SubmitPlan.Moved` with `movedSources`' concerns, so a submission nobody looks over, serve's or bump's, is held for it, and a person's submission shows it. Both walk the plan's Git-fetched targets, left-out ones included, in the plan's order (`plannedSources`).
- It's compared where submission plans, which reads the branch's edits already. A check's plan doesn't read edits, so `check --plan` doesn't say it.

## Not in this batch

- **A blocked result filled in from an earlier check** stands whatever its blocker's result does now. Where a Git-fetched target's tag moved, its old failure doesn't stand, but what it blocked still reads as blocked by it. A later check builds both. That a blocked result stands with what blocked it is a rule of its own.
- **A fetch failed as "the source moved"** in an earlier check stands for a later check that expects the commit it fetched, since its build recorded fetching that commit, though nothing was built from it. It then reads as failed, telling a person to run a new check, which builds it. `Counts` could refuse a result whose own check found its source moved (`recorded.GitIn(...).Moved(fetched)`).
- **`status`** doesn't show `release-moved` or `source-moved`: it reads recorded holds. `release-moved` needs only the store and the plan, so status could say it; `source-moved` reads the network. `status.go` was another session's here.

## Proven

- **Checks:** the full suite with `DOCKHAND_TEST_MACPORTS_TCLSH=/opt/local/bin/port-tclsh`, `go vet`, `make fmt-check`, `vendor-check`, `deadcode`, and `lint` pass.
- **New tests:**
  - `reuse`: the shared rule directly, a failed build, a build that didn't say what was active, and a target reached through what was active (`TestWhatWasBuiltAgainstAnotherSourceIsOneRule`); `Choose`'s tests pass unchanged;
  - the engine, through checks, their evidence, and a moved tag: the earlier-result test now has its dependents report libharbor active, and after the move harbor-cli and harbor-viewer ask for a check beside libharbor (`TestAnEarlierResultFillsInOnlyForTheCommitExpected`, the regression test); a left-out libharbor expecting its commit, and it and what was built against it asking for a check once the tag moves (`TestATargetLeftOutExpectsTheCommitItsTagNames`); a failure against the old commit not standing while a block does (`TestAnEarlierFailureAgainstAnOldCommitDoesntStand`); a provider that didn't say what was active (`TestADependentWhoseProviderDidntSayWhatWasActiveDoesntStand`); a build against a Git-fetched port its check didn't build (`TestABuildAgainstAGitFetchedPortItsCheckDidntBuildDoesntStand`); `Counts`' new case;
  - planning's sources for left-out targets, and none where one is ruled out; the plan's validation of them;
  - `Release.Names`; `preparedSources`' matching, newest update, and left-out targets; `movedSources` for a left-out target; and serve holding an update whose tag moved while its edit was prepared (`TestServeHoldsAnUpdateWhoseTagMovedBeforeItsCheck`), end to end.
- **Mutation testing:** 34 mutants, each flipping one decision and run against the tests that should catch it, the file restored from a copy after: the shared rule's transitivity, its reading of `builtAgainst`, `Choose` applying it, and `builtAgainst`'s commit; `readSources` reading nothing, judging failures or not, judging blocked results, the earlier plan's dependencies, the Git sources it gives the rule, the targets with no earlier result, the builds it gives it, and keeping what it finds; `Counts` ignoring it; planning's sources for left-out targets, and where they're ruled out, directly and through the engine; resolving only what's built; the plan's validation, three ways; `Release.Names`' ref, pinned commit, case, `.git`, archive, default branch, and repository; and `preparedSources`' comparison, newest update, port, source, a release or plan without a commit, left-out targets, and submission calling it. All are killed. One survived at first, a plan that resolved no commit being compared, and is killed by a case added for it.
- **Not run live.** Nothing here runs a guest. A `check --only` of one port of a branch with a Git-fetched port it doesn't need now reads that port's repository as the check is planned, which `-v` shows.
