# Unattended work

Choose how far the work should go and who will run it. `bump` carries one port
through to publication without prompts. `ship` can stop at an earlier stage and
can run with a person at a terminal. `check -d` queues a build in the
background. `serve` is the worker that continues accepting checks and, when
configured, looks for updates each day.

All examples below use an existing ports checkout:

```sh
export MACPORTS_TREE="$HOME/Source/macports-ports"
dockhand config
dockhand status
```

Replace the path with your checkout. Confirm the database and configuration
before starting a long run. If you have not set up a provider and GitHub login
yet, start with `dockhand usage`. For isolated experiments, follow the scratch
setup in `dockhand usage agents`.

## One port, without prompts

```sh
dockhand bump jq --plan
dockhand bump jq
```

The first command shows the proposed work. The second updates jq to the newest
release Dockhand can select, tidies the edit, checks its committed files, and
opens or updates its pull request when eligible. `bump jq 1.8.1` names a
particular version. This is not MacPorts' `port bump`: Dockhand's `checksums`
command refreshes checksums for the version already in the Portfile.

`bump` is the single-port unattended form of `ship`. It uses an existing open
branch changing the port when one can be selected, or starts a branch on fresh
master once there is an edit to make. It binds submission to the commit it
checked. If it stops, the report says whether nothing was changed or a branch
remains for inspection; `--json` retains the stages reached. A failed check
leaves completed results and logs, rather than discarding the branch.

A bump's destination is submission. To stop before publication, use `ship`:

```sh
dockhand ship jq --to commit --plan
dockhand ship jq --to commit --yes
dockhand ship jq --to check --yes
```

`commit` leaves the updated files committed. `check` also builds them and stops
before submission. Run these in order only if those are the stages you want;
they are not prerequisites for a direct bump. An already completed stage can
stand when you resume, and an active check is followed only when it covers the
request. `--build` or `--on` chooses the build location explicitly; `--tests`
chooses the test policy. See `dockhand usage checks` for those choices.

On a terminal, `ship` shows its initial plan and asks once before beginning,
with later review where needed. `ship --yes`, a ship without terminal input, and
`bump` all run unattended. Skipping the initial question does not accept
findings produced later: unattended work holds for a person's look.

## What an unattended hold means

Dockhand checks where it can build before editing. An already-current port
starts no new branch. An uncertain release selection, a new major version under
the default hold, or an existing competing pull request can stop an unattended
update before preparation. An inability to look for other pull requests can hold
it too; missing information is not treated as proof that none exists.

Later, a passing build can still leave a submission held. Archive comparisons,
source or ownership changes, commit-rule errors, an unreadable assessment, or
another pull request found before publication can need review. A build
establishes what happened on its selected environments; it does not settle those
concerns. Follow the named branch and the reported next step:

```sh
dockhand status --branch jq-update
dockhand submit --branch jq-update --plan
```

Use the actual branch name returned by your run. A held single bump normally
exits 3. A failed build exits 2. Neither means the branch should be started
again. Read `dockhand usage recovery` for the different ways to resume a stopped
check or fix a failed one.

A concern you have reviewed can receive an evidence-bound decision:

```sh
dockhand decide license-changed --branch jq-update jq \
  --because "the new license is acceptable and the Portfile identifies it"
```

Use the actual rule the report named, and your own reason. `--because` permits
unattended submission to proceed past that concern;
`--waiting "what must happen"` records that it remains blocked. Decisions stand
for that port's directory contents and lapse when those files change. They do
not turn a failed build into a passing one. `submit --accept` concerns eligible
build failures or lint findings and has different evidence requirements; see
`dockhand usage publishing`.

## Queue a check, or run a continuing worker

For one background build:

```sh
dockhand check --branch jq-update --head -d
dockhand wait check-42
```

Use the queued run's actual number. `-d` returns after queueing. A running serve
takes the work; otherwise Dockhand starts a background drain and reports its
PID. If starting that worker fails, the check remains queued and the message
tells you to run serve. `wait` follows it and, when nobody is running it, can
drive it here. A foreground check's interrupt stops work it is running locally
and preserves completed results; interrupting a follower leaves another worker's
check running.

For a worker that accepts new checks, first try it in a terminal:

```sh
dockhand serve --ship-to list
```

This runs queued checks, refreshes recorded pull-request state, and does the
configured daily look. `list` prepares no automatic updates and publishes none.
Leave it in a terminal of its own and inspect `dockhand status` from another.
One serve leads the repository's queue; another stands by for takeover. Ctrl-C
ends this foreground worker, leaving completed results for a later worker.
`serve --drain` instead runs queued work and exits when the queue drains; it is
not a replacement for a continuing service.

## Configure daily updates before enabling them

Edit the configuration file that `dockhand config` reports. For example, these
sections ask for checks of your maintained ports each morning, with no automatic
publication:

```toml
maintainer = "@your-github-login"

[build]
unattended = "minmax"

[serve]
ship_to = "check"
outdated_at = "07:00"
submit_limit = 2

[holds]
majors = "hold"
```

Merge these keys into existing sections rather than duplicating TOML tables.
Replace the maintainer identity with the spelling in your ports' maintainers
lines. `serve.maintainers`, `serve.categories`, and `serve.not_maintainers` can
select a different group; `dockhand usage batches` explains selection.

`build.unattended` is the profile for unattended updates. If unset, Dockhand
uses `minmax` when Tart is configured, otherwise the normal default profile. An
explicit `--build` or `--on` on ship or bump takes precedence. `holds.majors`
defaults to `hold`; allow rules and per-port choices are described in
`dockhand usage configuration`.

The daily stages are `list` (the default), `commit`, `check`, and `submit`. Only
`submit` enables automatic pull requests. Serve submits branches it prepared,
under its holds, and opens at most `serve.submit_limit` per day; the default is
ten. It leaves passing branches you started for you to ship. That limit is a
serve policy, not a cap on an explicit `ship` batch.

The daily look runs at the configured local time, or after that time when serve
next has the opportunity. Restarting serve, or changing `--ship-to`, does not
repeat a daily look already recorded as complete. To act on a selection now, run
an explicit `ship --mine --to check` or `ship --mine --to submit` after
inspecting its plan. Existing branches are best resumed by name. A rate-limited
daily look schedules one retry after the limit resets and reports it; repeated
restarts are not the remedy for a depleted allowance.

## Install, stop, and remove the service

After checking the foreground behavior, install the launchd agent on macOS:

```sh
dockhand serve --install
```

It starts at login and restarts if it stops. Installation reports the agent and
log paths. It retains the selected tree, database, Git executable, relevant
environment paths, and flags passed beside `--install`. If you used
`--ship-to list` to override a configuration that permits submission, include
that override in the installation too:

```sh
dockhand serve --install --ship-to list
```

Reinstall after changing the captured paths or flags. The agent retains the
configuration file's path; it does not embed the file's contents. Environment
tokens are omitted from its readable plist, so arrange the saved GitHub login
with `dockhand setup github` instead of assuming a shell's token follows it.
Notifications can be disabled with `--no-notify` for a run or
`serve.notify = false` in the file.

Ctrl-C stops a foreground serve. Killing an installed agent is temporary because
launchd restarts it. To remove the continuing service:

```sh
dockhand serve --uninstall
```

Removal waits for launchd to release the agent and stops automatic startup. It
does not delete branches, completed checks, or the database. Use
`dockhand usage recovery` for a check left stopped, and
`dockhand usage configuration` for the full list of service settings.
