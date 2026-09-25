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

[check]
on = ["github"]   # makes it check's default
```

## What it does

1. It pushes the commit to `dockhand-check/<commit>` on your fork. `check` says so before it starts. The push only happens if the branch isn't already there, and it is conditional on the branch's current head.
2. The workflow runs on that push, as it does for every branch but master. Dockhand waits up to 10 minutes for GitHub to start the run. If none starts, it says so and names your fork's Actions page.
3. It waits for the run to finish, then saves each runner's log beside the check's other logs (`dockhand logs check-N`).
4. It reads each log's markers for each port: the subport listing, `port lint` errors, failed dependencies and installs, and the install and test groups.
   - A port passes only if it passed on every runner.
   - If it failed on any runner, it failed at that runner's phase.
   - Test failures are advisory, as in MacPorts' CI, unless `--tests required`.

A run that was cancelled, timed out, or never started building is run again (only its failed jobs) instead of being read. So is a run that failed without naming a port, once: it counts as trouble with the environment, and a later attempt reruns it. A port's own failure is a verdict and is not retried.

`dockhand cancel` (or `check --replace`) cancels the run on GitHub too. A `serve` that stops leaves the run going, and the next `serve` picks it up again. Once a branch is merged, `clean` removes its `dockhand-check/` branches from your fork, as long as each still holds the commit that was checked.

The workflow builds only what the commit changes. A port that `--also` adds but the commit doesn't change isn't built there. Its result stays "not run", and the check says why.
