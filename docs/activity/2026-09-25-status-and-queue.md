# 2026-09-25: status, the attention list, `queue`, `wait`, `cancel`, and `logs`

Roadmap step 5, part 8, apart from `serve` (Design v3 §10, §11).

## The commands

- **`dockhand status`**, and bare `dockhand` in a ports checkout, starts with **Needs you**. Each row ends with the one command that moves it forward:
  - `✗` the latest check of the files as they are failed. The row names the port, the phase, and the environment: `dockhand logs check-N --port <port>`.
  - `!` the latest check needs attention: `dockhand logs check-N`.
  - `!` a snapshot passed, but the files have changed since: `dockhand check --branch <name>`.
  - `·` a snapshot passed but isn't committed: `dockhand tidy --branch <name>`.
  - `·` it passed and waits for you to submit, or the PR doesn't have it yet: `dockhand submit --branch <name>`.
  - `!` the branch's Git branch is gone.

  Below that is a table. Each column answers one question:
  - PORTS, by CI's rule;
  - WORK: commits, and uncommitted edits;
  - CHECKS: running or queued, else the latest result and whether it covers the files as they are now;
  - PR: number, draft, and whether it has the branch's head.

  The last line says whether `serve` runs and how many runs are queued.
- **Other forms of `status`:**
  - Inside a worktree, or naming a branch, `status` shows that branch in detail, with each target's result per environment and the next command.
  - `--attention` prints only the rows and exits 3 when there are any (§12).
  - `--port` narrows to the branches touching a port, and `--all` includes merged and archived branches.
- **`dockhand queue`** lists queued and running checks: run, branch, source, environments, state, detail. Runs `serve` started say so.
- **`dockhand wait <run>`** observes a run held by `serve` or another process. With no one running it, it runs the check here, as §11 says. A finished run is reported as it ended.
- **`dockhand cancel <run>`**:
  - A queued run is canceled at once.
  - A running one is stopped by whoever holds it, at its next step.
  - A run whose driver died is settled by `cancel` itself.
- **`dockhand logs <run>`** lists each execution's attempts, states, and each target's result and log. `--port` prints that port's log.

## Also changed

- **`tidy --squash --message`** is applied as given without a terminal: it is the person's own explicit plan, the alternative §8 gives to reviewing an ambiguous one. `--yes` still never resolves an ambiguous plan. A test using a squash in a script found the gap.
- **Not yet built:**
  - PR state from GitHub (reviews, CI, changes requested) in status. `serve` follows PRs and turns them into rows, and a `status --refresh` should too.
  - `watch`.
  - `--json`.

## Tests

- **Command tests** take one branch through its life:
  - nothing yet;
  - edits checked as a snapshot, so the row says to commit;
  - an edit after the check, so the row says files changed since, and `--attention` exits 3;
  - a hand edit making tidy ask for review, then a squash with a message;
  - a check of the commit, so the row says passed and waiting to submit, in the branch's detailed view.
- **Queue tests** cover:
  - two queued runs listed;
  - one canceled (and canceling it again refused);
  - the other run by `wait` with no serve;
  - `logs` listing and printing a port's log;
  - an unknown run refused.
