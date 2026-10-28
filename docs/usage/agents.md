# For scripts and agents

dockhand is used the same way by a person, a script, and an agent, and every command's `--help` is its reference. What differs without a person at a terminal is said here: how to read what a command did, what it asks and what it does instead, how to work without touching anyone's real setup, and which commands act beyond this Mac.

## Output and exit codes

- **Progress** goes to standard error as the work goes: what you need to follow it, such as a port index being built, which can take minutes. `-v` adds the work behind the scenes, and `-vv` every step. A check dockhand runs itself reports its own steps; `-v` shows what the engine and its providers say beneath them.
- **`--json`** writes one envelope on standard output when the command ends: `{"version": 1, "command": "...", "exit_code": 0, "error": null, "result": {...}}`. A command's result has one shape wherever it stops, and its exit code says where.
  - What it didn't reach is left out or empty.
  - The steps it goes on to are inside it:
    - `ship`'s and `bump`'s result is each change's update, under `changes`, with `tidy`, `check`, and `submit` inside;
    - `check`'s is the check's, with the `baseline` that `check.baseline` runs, whose `run.baseline_of` names the check it looks into.
  - A command that refuses before doing anything reports only its error.
- **Exit codes:** 0 for success, 1 for an error, 2 for a failed check, 3 when something needs attention (`status --attention`, a `bump` held for a look, an update that can't tell which release is the newest, a batch some of whose ports need a look, or `checksums --plan` finding distfiles that don't match), and 130 for an interrupt. A batch none of whose ports could be done exits 1.
- **`status --json`** gives each branch's `latest_check`, its newest finished check, and `active_checks`, those queued or running: a branch whose first check is still running has `latest_check` null and the check among `active_checks`. `check_standing` is what its checks establish of its files as they are now, the verdict status words: `not-checked`, `checking`, `stopped`, `passed`, `incomplete` (a pass that no longer stands everywhere it's required), `older` (a pass of other files), `failed`, or `attention`. `serve_state` gives serve's state as fields, beside `serve`'s line.
- **Long lists** a line would carry, as a check's changed ports and build order or an update's dependents, name their first few and count the rest, "18 ports: terraform-1.16, terraform-1.17, terraform-1.18 and 15 more"; `--json` has every one.
- **Without a terminal**, nothing is asked. A command that would ask refuses in one sentence, as "nothing was removed: without a terminal, --yes removes these", and exits 1, `clean` and `review` included, or proceeds where `-y` is given; a question with a safe way on, as stopping a running check a script replaces, takes it. `tidy` applies only a plan made of dockhand's own edits, or one you give it.

## Environment

| Variable | What it does |
| --- | --- |
| `MACPORTS_TREE` | the ports checkout, when `--tree` isn't given |
| `DOCKHAND_DB` | the database, when `--db` isn't given; `~/.dockhand/dockhand.db` otherwise. A newer dockhand migrates it forward, and an older one can't open it after: the migration says so, and keeps the database as it was beside it, `dockhand.db.schema-24` for one migrated from schema 24, for 30 days after a newer copy is made |
| `DOCKHAND_CONFIG` | the configuration file |
| `GIT_BIN` | the Git executable, when `--git` isn't given |
| `GH_TOKEN`, `GITHUB_TOKEN` | a GitHub token, ahead of any saved login |
| `DOCKHAND_TART_HOME` | where dockhand's Tart images are; `~/.dockhand/tart` otherwise |
| `TART_HOME` | your own Tart home, read to count your running VMs against the Mac's two; `~/.tart` otherwise |
| `DOCKHAND_SSH_DIR` | the keys dockhand reaches its Tart guests with; `~/.dockhand/ssh` otherwise |
| `DOCKHAND_UPSTREAM` | where master is fetched from, a mirror or a local repository, rather than MacPorts' own |
| `DOCKHAND_PULL_REQUESTS` | for testing dockhand: a sandbox, a fork of MacPorts' repository such as `you/macports-ports`, that pull requests go to instead, made within it. `submit`'s preview says so |
| `DOCKHAND_INDEX_MIRROR` | the directory port indexes are downloaded from, a nearer mirror's, rather than MacPorts' |
| `DOCKHAND_INDEX_CACHE` | where port indexes are cached; `dockhand/indexes` in your cache directory otherwise |
| `DOCKHAND_GITHUB_CLIENT_ID` | the OAuth application `setup github` uses |
| `DOCKHAND_GITHUB_LOG` | a file each command appends what it spent of GitHub's allowance to |
| `DOCKHAND_READING_CACHE` | where readings of upstream files are cached; `dockhand/readings` in your cache directory otherwise |
| `DOCKHAND_GITHUB_CACHE` | where GitHub's answers to reads are kept with their ETags, so a read asked again costs nothing of the hour's allowance where nothing moved; `dockhand/github` in your cache directory otherwise |
| `VISUAL`, `EDITOR` | the editor `edit` opens a Portfile in, and `submit` a pull request's description |

`serve --install` keeps the ones serve reads, as they are when it's run, in the agent: `DOCKHAND_CONFIG`, `DOCKHAND_UPSTREAM`, `DOCKHAND_PULL_REQUESTS`, `DOCKHAND_INDEX_MIRROR`, `DOCKHAND_INDEX_CACHE`, `DOCKHAND_READING_CACHE`, `DOCKHAND_GITHUB_CACHE`, `DOCKHAND_TART_HOME`, `TART_HOME`, and `DOCKHAND_SSH_DIR`, with `--git` and `--db`. A token is never kept there, since anyone on the Mac can read the agent's file.

## A scratch setup

Three variables say where dockhand works, and a run that mustn't touch a person's own work sets all three:

- `MACPORTS_TREE`, the ports checkout, which a shell may already point at the person's own; `--tree` overrides it;
- `DOCKHAND_DB`, the database of branches and checks, which a newer dockhand migrates forward and an older one then can't open; point it at a copy, made with `sqlite3 ~/.dockhand/dockhand.db ".backup <file>"`, or at a new file;
- `DOCKHAND_CONFIG`, the configuration file, whose `maintainer`, build profiles, and holds decide what a command does.

```sh
export MACPORTS_TREE=~/scratch/macports-ports DOCKHAND_DB=~/scratch/dockhand.db DOCKHAND_CONFIG=~/scratch/config.toml
dockhand setup -y
```

`DOCKHAND_PULL_REQUESTS` sends pull requests to a sandbox fork rather than to MacPorts, for testing dockhand itself.

## What reaches beyond this Mac

Most commands read and write only this Mac: the checkout, its worktrees, the database, and the checks' clones. These act elsewhere, under your GitHub login, and want a person's word before a script or an agent runs them:

- `submit`, and `ship` or `bump` carried to submit, push to your fork and open or update a pull request on `macports/macports-ports`, which notifies its maintainers; `submit --ready` takes one out of draft. Without a terminal, `ship` takes running it as agreeing to its plan, and `bump` never asks.
- `serve` with `serve.ship_to = "submit"` or `--ship-to submit` opens pull requests by itself, up to `serve.submit_limit` a day.
- `review <pr> --comment` and `--request-changes` post a review on someone's pull request.
- `retry --ci` runs a pull request's failed MacPorts CI jobs again.
- `check --on github` pushes a `dockhand-check/` branch to your fork and runs its Actions.
- `clean` deletes branches on your fork: a cleaned branch's, and those its GitHub checks left.
- `setup github` signs in through a browser.

Reading GitHub, as `outdated`, `update`, `status --refresh`, and `serve` do, spends your login's hourly allowance, which a person's own tools share; `outdated --mine` over hundreds of ports spends much of it. `--plan`, where a command has it, shows what it would do and does nothing.

