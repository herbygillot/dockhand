# 2026-09-27: update and bump share their code, and their JSON

The person asked whether `update` and `bump` share their logic in the right places, and then for them to return similar JSON. They didn't, quite.

- `bump`'s checks before the edit repeated, in the command layer, a decision the engine already makes for `update --outdated` and `serve` (`PlanOutdated`), and differed from it.
- `bump` looked up the newest release once to decide, then again to update.
- The two commands defined the same seven flags twice.
- Any update that went on to its pull request gave a script only the last step's JSON.

**A version update starts its branch once there is an edit to make.** `UpdateRequest.Start` prepares the update on master as fetched now. It starts the branch from that same master (`StartRequest.Base`) only when there is something to write.
- A port already current starts nothing. Before, `update --new` left an empty branch behind, and so did `serve`'s preparation, should a lookup and an update ever disagree.
- An edit dockhand can't make by itself still starts the branch, for the person to make it by hand, as before.

`update --new`, `bump`, and `PrepareOutdated` all use it. So `bump` no longer looks up the release first: whether the port is current comes from the update itself, with its real version and revision. This replaces the recommendation to share `PlanOutdated`'s decision with `bump`, since it removes the duplicate rather than sharing it.

**A branch changes a port with its working files too.** `BranchesChanging` counted commits only, but `update` commits nothing. So after `update jq --new`, serve's daily look, or `update --outdated`, would have started a second jq branch beside it. It now counts the edits in a branch's worktree, when the branch is checked out there. `bump`'s refusal uses it, rather than its own scan of every branch's status.

**One path in the command layer.** `versionUpdate` holds what `update` and `bump` share:
- its flags (`--on`, `--tested-*`, `--keep-old-checksums`, `--shared-release`, `--revbump-dependents`, `--except`);
- the request they make, with its `--except` check;
- `run`, which goes through `submitReady`, `author`, and `tidyAndSubmit`.

`submitReady` checks, before the edit, where to check, and for `bump` that no open branch changes the port. `bump` is now its help text, and the options it sets.

**One JSON result for an update that goes on.** With `--json`, `update --submit` and `bump` report the update's result, with `tidy`, `check`, and `submit` inside it, each as that command reports it, as far as it went.
- A script reads the versions, the upstream comparison, the check's results, and the pull request from one result.
- A failed check leaves `submit` absent.
- A held `bump` has `submit.held` saying why, with `pull_request` null.

The steps are gathered by `Streams.linkSteps`: each later `emit` lands in the update's result. The tidy plan and the submit preview are now emitted before they are acted on, so a refusal still shows them.

**The difference between the two.** Only in what `bump` does, never in the result's shape:
- `bump` never asks, so the checkboxes follow the flags alone.
- `bump` refuses a port another open branch changes.
- Only `bump`'s submission is held, so only its `submit` can carry `held`.
- `update --submit` without a terminal isn't held: running it is the decision, as for `submit --check`.

**Tests.**
- `TestAnUpdateStartsItsBranchOnlyForAnEdit` covers the deferred start (current, new, and by hand), and the uncommitted edit that `BranchesChanging` and `PlanOutdated` now see. It fails without the worktree's edits.
- `TestUpdateSubmitAndBumpReportTheSameJSON` runs both commands to a pull request and a held `bump`. It fails without `linkSteps`.
- `TestBumpChangesNothingWhenItHasNothingToDo` now reaches "current" through the update, and checks no branch is started.

`submit --check --json` still reports its last step alone, as `update --submit` did. The person decided to leave it: a command with several stopping points may report each through its exit code and a result of that step's shape, and a uniform shape isn't sought for its own sake. What matters is sharing code wherever it can be shared. The scripting section of the usage guide now says so.

Revised the same day: the person's rule is one shape per command wherever it stops, and `submit --check` now reports the submission's result with its check inside ([note](2026-09-27-one-json-shape.md)).
