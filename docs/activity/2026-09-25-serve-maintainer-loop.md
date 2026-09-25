# 2026-09-25: serve.for_outdated, serve --submit-passing, and notifications

Step 11 of the roadmap, the maintainer loop, second half: Design v3 §11's list of what `serve` does besides running checks.

## What changed

- **`[serve]` in the configuration file**, each value checked:
  - `for_outdated = "list" | "draft" | "check"` (list by default);
  - `outdated_at = "07:00"`;
  - `submit_passing = false`;
  - `submit_limit = 10`;
  - `notify = true`.
- **Your outdated ports, once a day.** At `outdated_at` or later, `serve` looks for new releases of the ports your `maintainer` line names, once per database per day, by a stamp beside the database.
  - `list` records what it found. `status` shows "Your ports: N have newer releases, as serve found … (dockhand update --outdated --mine)".
  - `draft` prepares a branch for each, as `update --outdated` does: one per port, updated, with the upstream archives compared, and committed.
  - `check` also queues a check of each, which `serve` then runs.

  Branches `serve` starts are recorded as its own. Without a `maintainer`, it says what to set, once.
- **`serve --submit-passing`** opens pull requests, or `serve.submit_passing = true` for every run, and `--no-submit-passing` turns it off for one. It says at startup that it opens PRs for passing updates, and at most how many a day. The leading serve records that beside the database, so `status` and `queue` in other terminals say it too. The guardrails (`Engine.ServeCandidates`):
  - **Scope.** Only open branches `serve` started itself, with no pull request yet.
  - **What passed.** The latest check passed for exactly the committed files, with nothing uncommitted. Anything `submit`'s preview would block, including a failure that would need `--accept`, holds the branch.
  - **Held for a look.** A `!` finding from the upstream comparison, or any commit-rule finding, holds it. `status` shows the branch on the attention list: "passed; held for a look: …".
  - **A daily limit.** At most `submit_limit` a day, counted beside the database across restarts. The rest wait until tomorrow, and serve says so.
  - **Visibility.** The pull request's description ends by saying serve opened it without a person's review. Its tested-binaries and variants boxes stay unchecked, since only a person can say those. The attention list shows it as "opened by serve".
- **Notifications.** With `serve.notify` on, `serve` posts a macOS notification through `osascript` when a check it runs finishes, when a pull request changes, and when it prepares or opens something. Elsewhere, and in tests, nothing is posted.

## Tests

- **Config:** defaults, every key set, and each bad value refused by name.
- **Engine:**
  - serve prepares and checks jq's update; only that branch is a candidate, not a person's branch;
  - submitting it puts the note and the unchecked boxes in the description, and then it is no longer a candidate;
  - with the upstream license changed, status and the candidate are held, and submitting it is refused.
- **Command:**
  - a morning `serve --submit-passing` with `for_outdated = "check"` finds jq, prepares it, checks it, opens #34901, and posts notifications;
  - `status` shows "opened by serve" and your ports' count;
  - a second `serve` the same day doesn't look again, and without the flag opens nothing;
  - with `submit_limit = 1` and one pull request already opened today, serve holds the next until tomorrow;
  - the serve tests pass repeatedly under the race detector.

**Not yet:**
- `queue pause` and `resume`.
- The design's "keeps master fresh and marks branches that no longer rebase cleanly".
- Notifications for a person's own foreground checks, which report in their terminal.
