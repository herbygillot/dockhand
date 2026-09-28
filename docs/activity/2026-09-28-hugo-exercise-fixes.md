# 2026-09-28: what the hugo exercise found

Another session took hugo from 0.166.0 to 0.167.0 with dockhand, as a maintainer would, through to macports-ports#35000, and wrote down what got in the way ([review](../reviews/2026-09-28-hugo-bump-exercise.md)). Each finding was checked against the code before anything was changed, and each fix is its own commit.

## What comes next, after update and check

`update` ended with "Next: review it with git diff, then commit it", which sends a person to commit by hand when `tidy` is what commits. `check` ended with "Passed for snapshot 1." and nothing after it. Design v3 §6.2 has each say what follows, and the code had drifted from it.
- `update`, and the other edits that share its ending, now says "Next: dockhand check". For a branch other than the one checked out here, as `--new` starts, it says to go there first: `cd "$(dockhand path jq-4k2p)", then dockhand check`, since a check from elsewhere takes the branch's committed head.
- A `check` that passed ends with what `status` would say moves the branch on: "Next: dockhand tidy --branch jq-update" for uncommitted work, or `submit` for a commit. A check that `submit --check`, `update --submit`, or `bump` runs leaves that to the submission that follows.

Tests fail with each part undone: `TestUpdateInTheBranchCheckedOutHere` and `TestUpdateWithoutABranchAsksOrSaysHow` for the two forms of update's line, `TestCheckRunsHereWithoutServe` for check's, and `TestSubmitCheckPassingAndReady` for its absence under `submit --check`.

## A check made before tidy is the commit's

Checking before tidying, the order the design gives, left hugo's branch at "passed for snapshot 1" in `status`, even with its pull request open. `submit` credited the same check to the commit, "for this commit's files", since a check is keyed to the files and tidy committed them unchanged.

`status` now says "passed for this commit" whenever the latest check read exactly the files as they are, and none of them are uncommitted. Edits on top of the commit still make it the snapshot's. `TestStatusCreditsACheckOfTheCommittedFilesToTheCommit` covers both, and fails with either undone.

## `logs` without a check

`dockhand logs` with nothing named gave cobra's "accepts 1 arg(s), received 0". In a branch's worktree it now shows the branch's latest check, the one a person just ran. Elsewhere it says what to name, and a branch with no check says so. Its heading no longer repeats a passed check's state as its detail: "check-6 · passed", not "check-6 · passed: passed".

`TestQueueWaitCancelAndLogs` covers each, and fails with each undone: the latest check rather than an older one, both messages, and the heading.

## Upstream, once

The submit preview's new Upstream line read "Upstream · upstream: go.mod moves …", and duckdb's "Upstream ! upstream's CMakeLists.txt changed". Update's own "Upstream changes:" did the same. A finding's words stand alone where they're used, as in what holds a submission, so they say whose change it is.

Under an Upstream label or heading, a finding now leaves that out: "Upstream ! CMakeLists.txt changed; the build may need the Portfile to follow". `submit --passing`, whose lines have no heading, keeps it. `TestUpstreamIsSaidOnceUnderItsHeading` and `TestTheSubmitPreviewGivesEachUpstreamFindingALine` fail with any of it undone.

## A plan with no branch

`update hugo --plan` in the main checkout refused, "hugo is in no open branch, and this checkout is on none", unless `--new` was given, though a plan changes nothing. On a terminal it offered to start a branch for it.

A version update's plan with no branch to plan in now plans on master, as `--new --plan` does: none named, nothing tracked checked out here, and no open branch changing the port. Where a branch changes the port, it still says which to name. On a terminal a plan now asks nothing that could start a branch. Someone's untracked branch is still theirs to adopt, and a branch named with `--branch` is still where the plan goes.

`TestUpdateWithoutABranchAsksOrSaysHow`, `TestAPlanInANamedBranchIsPlannedThere`, and `TestAnUntrackedBranchHereIsTheirsToAdopt` fail with any condition undone.

## Log directories name macOS's release

Everything dockhand says names the release by macOS's number, macOS 26 (Tahoe), but a check's log directory was `tart-25-arm64-1`, by the Darwin version. It's now `tart-macos26-arm64-1`. A Darwin release dockhand doesn't know keeps its Darwin version, as `darwin30`, as its headings do. Each result records its log's whole path, so the logs of earlier checks are found where they are. `TestLogDirectoriesNameTheMacOSRelease` fails with either part undone.

## A pull request's CI, before anything has read it

Once #35000's CI had passed, `status` still showed only "#35000". It does show CI, "#35000, CI ✓", once `status --refresh` or `serve` has read the pull request from GitHub, but nothing said so, and plain `status` never reads GitHub. A branch's own status now says how while nothing has: "#35001 · CI not read yet (dockhand status --refresh)". The table of all branches keeps its short cell. `TestStatusRefreshShowsWhatTheReviewersSaid` fails without the hint, and with it left after the read.

## A Generated-By nobody can find

Each commit tidy writes names the dockhand that wrote it, in its `Generated-By` trailer, by the version Go stamped. A build of uncommitted source carries `+dirty` there, and such a trailer went out three times in one day:
- #34992 was merged with a `+dirty` version in its commit;
- duckdb's first tidy named one, and was redone;
- the hugo branch was tidied with a build of `14320eb7`, which a rebase of the shared main then orphaned.

Two checks now catch the first kind:
- **Tidy** warns as it writes such a trailer, with how to redo it: "Before submitting, tidy again with a build of a pushed commit: dockhand restore tidy-3, then dockhand tidy."
- **Submit's preview** flags each commit whose trailer names one, however it got there, and `submit --json` lists them as `modified_builds`. A person's own submission isn't held for it.

A build is of uncommitted source when its tag carries the toolchain's `+dirty`, or an untagged build's `.modified` (`version.TagModified`).

The orphaned commit isn't caught: that would take asking GitHub whether the commit exists, from tidy, which reads nothing remote. The way to avoid it is written down in development.md: build from a pushed commit, in a clean worktree. This session also no longer rebases the shared main.

Tests fail with each part undone:
- `TestATagOfUncommittedSourceIsModified` for both forms;
- `TestAModifiedBuildIsKnownFromItsTrailer`, only dockhand's own trailer counting;
- `TestSubmitShowsACommitNamingAModifiedBuild` for the preview and JSON;
- `TestTidyWarnsOfAModifiedBuild`.

## `check --plan` records nothing

`check --plan` says it "changes nothing", but it printed "captured working files as snapshot 1": it recorded the snapshot, numbered it, and the check that followed reused it. A plan now captures without recording (`CaptureRequest.Plan`):
- it still finds a snapshot already recorded of the same files, "checking snapshot 1";
- new files are "a new snapshot", which only a check records and numbers: "would capture the working files as a new snapshot".

The unrecorded capture gets an ID of its own, as the plan made of it already did, since a plan names its revision.

`TestCheckRunsHereWithoutServe` and `TestJSONEnvelopes` fail with any part undone. The check after a plan now captures snapshot 1 itself, rather than reusing one the plan left behind.
