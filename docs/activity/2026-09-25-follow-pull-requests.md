# 2026-09-25: following pull requests

Design v3 §6.10, §6.13, and §11's "follows PRs": dockhand now reads what GitHub says about your pull requests.

## What changed

- **`status --refresh`, and `serve` every five minutes**, read each open or closed branch's pull request: its state, then for an open one its reviews and checks. It uses v2's GitHub client, which already inspected reviews (the latest from each reviewer), check runs, and commit statuses. What changed since the last reading is printed and recorded as events. A PR that can't be read is reported, and the rest are read. `serve` reports each problem once until it changes, so a missing login doesn't repeat every five minutes.
- **State follows the pull request:**
  - Merged marks the branch merged. It leaves the open list and stays in `status --all` (§6.13).
  - Closed marks it closed, and reopened marks it open again.
- **The attention list gains three rows**, each with its age, since observed forge state shows how old it is (§10):
  - `!` someone else pushed to the PR: it holds a commit dockhand didn't push and the branch doesn't have. Next: `dockhand submit`, which stops and shows the comparison.
  - `!` changes requested. Next: `dockhand edit <port>`, as in §6.10.
  - `✗` MacPorts CI failing, naming the checks. Next: the PR's checks page.
- **The PR column** shows the review and CI (`#34901 changes requested, CI ✓`), or the state once the PR is merged or closed.
- The observation is kept on the branch (schema 4's `pr_observed`), and draft state comes from GitHub too.

## Tests

- **Engine tests** use the stand-in GitHub and the local fork from the `submit` tests:
  - requested changes and a failing check, read and recorded;
  - a second reading with nothing new saying nothing;
  - another clone pushing to the PR's branch, seen as someone else's push;
  - a merge marking the branch merged and dropping it from the open list, while it stays searchable.
- **Command tests:**
  - `submit`, then `status --refresh`, showing the requested changes as a row and in the PR column;
  - a running `serve` noticing CI failing by itself, then `status --attention` exiting 3 with the failing check named.

**Not proven here:** the real GitHub API, which v2's client already talked to, and the review-request prompt of §6.10.
