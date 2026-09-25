# 2026-09-25: the github provider

Design v3 §7: `--on github` builds with MacPorts' own workflow, in your fork. This completes that roadmap item. The user guide is [github-provider.md](../github-provider.md).

## What changed

- **`internal/provider/actions`**, the provider.
  - It pushes the job's commit to `dockhand-check/<commit[:12]>` on your fork. It skips the push when the branch already holds that commit, and conditions it on the observed head.
  - It finds the workflow's push run of that commit, waiting up to 10 minutes for one to appear. It then waits for the run to finish, reporting its state as progress.
  - It saves each job's log in the execution's directory and reads it.
  - `logs.go` reads the markers `main.yml` writes, in either spelling: the workflow's own (`::group::`, `::error file=…::`) and GitHub's rendered one (`##[group]`, `##[error]`, behind timestamps). Nothing beyond the markers is inferred.
  - A port's verdict needs every runner:
    - it failed if it failed on any runner, at the first failing runner's phase;
    - it passed if it passed on all of them;
    - if a runner listed it but never reached it, it gets no verdict.
    - Tests failing on any runner are recorded as failed; they are advisory under the default policy, as in MacPorts' CI.
  - `github.go` is the go-github client. It uses your login, and downloads logs through `fetch` without the API's credentials, bounded at 64 MiB.
- **Retries.** Engine retries of an execution map onto GitHub's "re-run failed jobs":
  - A run that ended cancelled, timed out, stale, or at startup is rerun instead of read.
  - A failed run that attributes no port is `ErrInfrastructure`, and the next attempt reruns it.
  - A port's own failure is a verdict, and the runner never repeats a verdict.
- **Finding your fork.** It is now `Engine.Fork`, shared with `submit`. It finds the one remote that pushes to a fork of `macports/macports-ports` your login owns, or the remote named in `[providers.github] remote`.
- **`check`** accepts `--on github` and refuses releases with it: the workflow's matrix picks the runners. `check` and `submit --check` show a `Pushes` line whenever a check will push to your fork, as §9 requires ("only checking never hides a write to your fork"). With no provider set up, the error now names both `github` and `command`.
- **`serve`** lists github among the providers it builds on, since it needs no setup beyond a login.

## Decisions

- **The github provider is always registered.** It needs no configuration, only a login and a fork, and it never becomes the default by itself. A check pushes to your fork only when you name `--on github` or put it in `check.on`.
- **No platform on the environment.** The runners are whatever MacPorts' matrix says today. Recording a release dockhand didn't choose would make an old result look more specific than it is. The per-runner logs say which runner said what.
- **Ports the workflow didn't build stay "not run"**, with a progress line explaining why, rather than being given a verdict. That applies to `--also` dependents, for example. A check that includes them needs attention, which is honest: CI said nothing about them.
- **A reused run is read, not re-run.** If the commit's branch already has a successful or port-failing run, a new check of the same commit reads it. The run is the evidence for that exact commit.

## Left for later

- `clean` doesn't yet remove `dockhand-check/` branches from your fork. (Done the same day, [note](2026-09-25-github-clean-and-cancel.md).)
- Cancelling a check doesn't cancel the GitHub run. (Done the same day, same note.)
