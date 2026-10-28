# Scripts and agents

The Dockhand binary contains both the workflow guide and the command reference.
Start with `dockhand usage` for setup and an update through to its pull request.
Use `dockhand usage checks` for building and diagnosis, and
`dockhand <command> --help` for that command's arguments and flags. A source
checkout of Dockhand is not required.

A useful first inspection is:

```sh
export MACPORTS_TREE="$HOME/Source/macports-ports"
dockhand config
dockhand status --json
dockhand outdated jq --json
```

Use your actual ports checkout in the first line. `config` shows the
configuration file and database selected; `status` shows recorded work;
`outdated` looks upstream for a release. Run `dockhand usage branches` before
choosing a branch for an existing change. Keep the same tree, database, and
configuration throughout a workflow: a check number belongs to that database and
repository, rather than identifying a check everywhere.

## Choose the scope before acting

A script can use the normal setup, with its existing branches, saved GitHub
login, and build images. For an experiment, make a separate ports clone,
database, configuration, and worktree directory. Setting only `--db` does not
isolate Git changes, and setting only the tree does not isolate the database or
the directory where new worktrees go.

This example creates a scratch setup from a full ports clone:

```sh
scratch=$(mktemp -d "${TMPDIR:-/tmp}/dockhand-XXXXXX")
git clone https://github.com/macports/macports-ports.git "$scratch/ports"
mkdir -p "$scratch/state" "$scratch/worktrees"
cat > "$scratch/config.toml" <<EOF_CONFIG
worktrees = "$scratch/worktrees"

[cleanup]
automatic = false
EOF_CONFIG
export MACPORTS_TREE="$scratch/ports"
export DOCKHAND_DB="$scratch/state/dockhand.db"
export DOCKHAND_CONFIG="$scratch/config.toml"

dockhand config
dockhand update jq --new --plan
```

The configuration report should name the scratch database. The update preview
plans the edit without starting its branch. The explicit `worktrees` setting
keeps a later real update out of your normal worktree directory. Keep the shell
open or save the three exported paths before returning to this experiment.

This isolates the work records and Git workspace. It does not create a new
GitHub identity or a separate Tart installation. Commands still use the
available credentials, build providers, and caches unless configured otherwise.
To build, follow `dockhand usage` and `dockhand usage configuration` for the
provider you intend to use. `DOCKHAND_PULL_REQUESTS` selects a sandbox
repository for testing publication; inspect `submit --plan` to confirm its
destination before publishing there.

## Read the result, including partial progress

Progress goes to standard error. Standard output contains the human report, or,
with `--json`, one JSON envelope when the command finishes:

```json
{"version":1,"command":"check","exit_code":0,"error":null,"result":{}}
```

The result above is only an illustration of the envelope; an actual check
includes its branch, revision, plan, run, and target results. Keep standard
error separate when parsing standard output. `-v` adds detail about the work,
and `-vv` adds individual steps. Long human lists can be shortened; their JSON
lists retain all entries.

Inspect both `exit_code` and `result`. A nonzero exit does not mean nothing
happened. An update may have left an edited branch, a check may have completed
some builds, and a submission may have reached a later operation before it
stopped. A refusal before any result is available can have `result: null`.

Composite commands gather their stages. `ship` and `bump` return a `changes`
list, with each change's update and the `tidy`, `check`, and `submit` stages it
reached. A stage that was not reached is absent or empty. A check can contain
the `baseline` that `check.baseline` ran after it; that baseline's
`run.baseline_of` identifies the original check. Do not infer completion from
the presence of a branch alone.

The process exit codes are:

| Code | Meaning |
| --- | --- |
| 0 | The command completed successfully, including a successful no-op. |
| 1 | An error or refusal; also a batch where none of the requested work could complete. |
| 2 | A check failed. |
| 3 | Attention is needed: for example a held bump, an uncertain release, a partially successful batch, or a checksum preview finding a mismatch. |
| 130 | The command was interrupted. |

`status --json` distinguishes a branch's newest finished check, `latest_check`,
from its `active_checks`. The first check can still be running while
`latest_check` is null. `check_standing` gives the verdict for the current
files: `not-checked`, `checking`, `stopped`, `passed`, `incomplete`, `older`,
`failed`, or `attention`. Use the reported branch and run names for the next
inspection.

## Terminal input and consent

Without a terminal, Dockhand asks no questions. The next behavior depends on the
command, rather than on a universal meaning of `--yes`.

`submit` and an interactive `clean` operation normally need `--yes` without a
terminal. `review --comment` and `review --request-changes` explicitly select a
posting action. `tidy` can apply Dockhand's own unambiguous edits without
asking. A plan needing review stops unless you explicitly supply its grouping or
approve the displayed plan with tidy's own `--yes`. When `check --replace` needs
to stop a running check, a script asking for replacement permits that stop.

`ship` is different: running it without a terminal, or passing `--yes`, selects
unattended execution. It continues under the unattended holds, and does not
interpret `--yes` as accepting every finding that appears later. `bump` always
uses that unattended path. An attended `ship` asks about its initial plan and
can ask for review of later work. Read `dockhand usage unattended` before
choosing between these modes.

`--json` makes the command noninteractive too. Do not add it to a command
expecting to answer a prompt. Select the intended flags first, then request JSON
for the result.

## Preview and inspect the next step

Where available, `--plan` previews the requested operation without applying its
edits, commits, builds, publication, or removal. A preview may still fetch
master, look up releases or pull requests, download or cache information, and
open the database. It is not an offline mode or a promise that no supporting
files will be written. Use a scratch database if opening your normal database
with another build is itself unwanted; schema migrations go forward, and older
binaries may then require the kept pre-migration copy.

After a hold or failure, inspect instead of retrying blindly:

```sh
dockhand status --branch jq-update --json
dockhand explain check-42
dockhand logs check-42
```

Replace those names with the ones the command returned. A stopped check may need
`wait`; a failed build may need an edit and a new check; an upstream concern may
need review and a recorded decision. Repeating an unchanged request does not
resolve those different causes. See `dockhand usage recovery` for resuming,
replacing, and abandoning work, and `dockhand usage publishing` for publication
checks and accepted failures.

## Actions beyond the checkout

Some commands use the current GitHub login to act remotely. `submit`, and `ship`
or `bump` reaching submission, push to a fork and open or update a pull request.
`serve --ship-to submit` can do that for branches it prepared, within its daily
limit. `review --comment` and `--request-changes` post reviews; `retry --ci`
reruns failed CI jobs; `check --on github` pushes a check branch and runs
Actions. Cleanup can remove remote branches it offers. `setup github` performs
sign-in and saves credentials.

Choose those commands and destinations deliberately in the calling workflow.
Dockhand's flags and unattended holds determine whether they proceed; there is
no additional agent-specific approval mode. Even commands that only read
upstream can spend the login's shared GitHub allowance. `dockhand usage batches`
explains large selections and rate limits.

## Environment

Flags override the corresponding environment selections. These variables are the
complete catalog beyond the usual `HOME`, `PATH`, and `XDG_CACHE_HOME`.

| Variable | What it selects |
| --- | --- |
| `MACPORTS_TREE` | Ports checkout, unless `--tree` is given. |
| `DOCKHAND_DB` | Database, unless `--db` is given; otherwise `~/.dockhand/dockhand.db`. |
| `DOCKHAND_CONFIG` | Configuration file; otherwise `~/.dockhand/config.toml`. |
| `GIT_BIN` | Git executable, unless `--git` is given. |
| `GH_TOKEN`, `GITHUB_TOKEN` | GitHub token, ahead of the saved login. |
| `DOCKHAND_TART_HOME` | Dockhand's Tart images; otherwise `~/.dockhand/tart`. |
| `TART_HOME` | Your Tart home, consulted when counting running VMs; otherwise `~/.tart`. |
| `DOCKHAND_SSH_DIR` | Keys for Dockhand's Tart guests; otherwise `~/.dockhand/ssh`. |
| `DOCKHAND_UPSTREAM` | Source to fetch master from, such as a mirror or local repository. |
| `DOCKHAND_PULL_REQUESTS` | Sandbox repository for testing pull-request publication. |
| `DOCKHAND_INDEX_MIRROR` | Mirror from which port indexes are downloaded. |
| `DOCKHAND_INDEX_CACHE` | Port-index cache; otherwise `dockhand/indexes` in the cache directory. |
| `DOCKHAND_READING_CACHE` | Upstream-reading cache; otherwise `dockhand/readings` in the cache directory. |
| `DOCKHAND_GITHUB_CACHE` | GitHub response/ETag cache; otherwise `dockhand/github` in the cache directory. |
| `DOCKHAND_GITHUB_CLIENT_ID` | OAuth application used by `setup github`. |
| `DOCKHAND_GITHUB_LOG` | File to append each command's GitHub request spending to. |
| `VISUAL`, `EDITOR` | Editor for Portfiles and pull-request descriptions. |

`serve --install` keeps `DOCKHAND_CONFIG`, `DOCKHAND_UPSTREAM`,
`DOCKHAND_PULL_REQUESTS`, `DOCKHAND_INDEX_MIRROR`, `DOCKHAND_INDEX_CACHE`,
`DOCKHAND_READING_CACHE`, `DOCKHAND_GITHUB_CACHE`, `DOCKHAND_TART_HOME`,
`TART_HOME`, and `DOCKHAND_SSH_DIR` in the agent, with its PATH and explicit
tree, database, and Git selections. It does not write a token into the agent
file. Read `dockhand usage configuration` for settings and
`dockhand usage unattended` for installing and stopping the service.
