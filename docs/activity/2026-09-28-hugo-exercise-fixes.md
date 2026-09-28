# 2026-09-28: what the hugo exercise found

Another session took hugo from 0.166.0 to 0.167.0 with dockhand, as a maintainer would, through to macports-ports#35000, and wrote down what got in the way ([review](../reviews/2026-09-28-hugo-bump-exercise.md)). Each finding was checked against the code before anything was changed, and each fix is its own commit.

## What comes next, after update and check

`update` ended with "Next: review it with git diff, then commit it", which sends a person to commit by hand when `tidy` is what commits. `check` ended with "Passed for snapshot 1." and nothing after it. Design v3 §6.2 has each say what follows, and the code had drifted from it.
- `update`, and the other edits that share its ending, now says "Next: dockhand check". For a branch other than the one checked out here, as `--new` starts, it says to go there first: `cd "$(dockhand path jq-4k2p)", then dockhand check`, since a check from elsewhere takes the branch's committed head.
- A `check` that passed ends with what `status` would say moves the branch on: "Next: dockhand tidy --branch jq-update" for uncommitted work, or `submit` for a commit. A check that `submit --check`, `update --submit`, or `bump` runs leaves that to the submission that follows.

Tests fail with each part undone: `TestUpdateInTheBranchCheckedOutHere` and `TestUpdateWithoutABranchAsksOrSaysHow` for the two forms of update's line, `TestCheckRunsHereWithoutServe` for check's, and `TestSubmitCheckPassingAndReady` for its absence under `submit --check`.
