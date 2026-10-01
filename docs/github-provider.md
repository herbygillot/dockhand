# The github provider

`check --on github` builds a revision with MacPorts' own workflow (`.github/workflows/main.yml`), in your fork of `macports/macports-ports`. It builds the way MacPorts' CI builds: the ports the commit changes, on the macOS releases its matrix names, with default variants, `port lint` without `--nitpick`, and the built-in test phase (Design v3 §7).

## Setting it up

- A GitHub login: `dockhand auth login`.
- Your fork, with a Git remote that pushes to it: `git remote add fork https://github.com/<you>/macports-ports.git`.
- Actions enabled for your fork, on its Actions tab. GitHub turns them off for new forks.

If more than one remote pushes to a fork you own, name one in `~/.dockhand/config.toml`:

```toml
[providers.github]
remote = "fork"
capacity = 2      # how many checks serve runs on it at once; 2 when unset

[check]
on = ["github"]   # makes it check's default
```

## What it does

1. It pushes the commit to `dockhand-check/<commit>` on your fork. `check` says so before it starts. The push only happens if the branch isn't already there, and it is conditional on the branch's current head.
2. The workflow runs on that push, as it does for every branch but master. Dockhand waits up to 10 minutes for GitHub to start the run. If none starts, it says so and names your fork's Actions page.
3. It waits for the run to finish, then saves each runner's log beside the check's other logs (`dockhand logs check-N`).
4. It reads each log's markers for each port: the subport listing, `port lint` errors, failed dependencies and installs, and the install and test groups.
   - A port passes only if it passed on every runner that built it.
   - If it failed on any runner, it failed at that runner's phase, and the result names the runner.
   - A runner that listed its subports without the port didn't build it, as the workflow leaves a port off a macOS it doesn't support. The others decide.
   - Each runner's part is kept with the result: `dockhand logs check-N` shows it under the port, and `--json` as `builders`.
   - Test failures are advisory, as in MacPorts' CI, unless `--tests required`, which dockhand applies to the workflow's reported results as it does to Tart's. The workflow runs its own tests whatever the policy, so `--tests skip` only stops them counting.
5. It removes `dockhand-check/<commit>` from your fork, as long as it still holds the commit pushed. The logs are kept here, so nothing needs the branch once the run is read. `check` says this too before it starts.

A run that was cancelled, timed out, or never started building is run again (only its failed jobs) instead of being read. So is a run that failed without naming a port, once: it counts as trouble with the environment, and a later attempt reruns it. A port's own failure is a verdict and is not retried.

The branch stays while the check may still need its run: until the check's last attempt, since a later attempt runs the run again, and when a `serve` stops, which leaves the run going for the next `serve` to pick up. A later check of the same commit, such as `dockhand retry`, pushes it again and reads the run that push starts, not the earlier one.

A branch dockhand can't remove, because your fork can't be reached, say, is said once, and the check goes on as it would. `dockhand cancel` (or `check --replace`) cancels the run on GitHub too, and leaves the branch, so a retry runs that run again. Once a branch is merged, `clean` removes the `dockhand-check/` branches its checks left on your fork, as long as each still holds the commit that was checked.

The workflow builds only what the commit changes. A port that `--also` adds but the commit doesn't change isn't built there. Its result stays "not run", and the check says why.

## A port fetched with Git

A check expects a Git-fetched port's build to fetch the commit its `git.branch` names when the check is planned, since a tag can be moved and binds nothing, as an archive's checksums do. Tart checks that commit in the guest, and a command reports what it fetched. MacPorts' workflow says neither, so this provider can't attest which commit a runner built, and doesn't claim to: the check says so as it records the port's result, and `dockhand logs check-N` says "which commit of git.branch … it fetched isn't known: its provider didn't say" under the port. Such a result stands for its own check alone. No later check reuses it, nor does a later check of the same files take it in place of a build of its own.
