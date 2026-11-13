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

dockhand plans on master as it fetches it from MacPorts, not on the scratch
clone's own master, so a scratch run that replays an older tree also sets
`DOCKHAND_UPSTREAM` to a repository whose master is that older commit, such as
a bare clone sharing the scratch clone's objects; otherwise the plan reads
today's Portfile.
To build, follow `dockhand usage` and `dockhand usage configuration` for the
provider you intend to use. `DOCKHAND_PULL_REQUESTS` selects a sandbox
repository for testing publication; inspect `submit --plan` to confirm its
destination before publishing there.

## Read the result, including partial progress

Progress goes to standard error. Standard output contains the human report, or,
with `--json`, one JSON envelope when the command finishes:

```json
{"version":1,"command":"check","exit_code":0,"error":null,"result":{},"github_requests":0}
```

The result above is only an illustration of the envelope; an actual check
includes its branch, revision, plan, run, and target results, and
`github_requests` counts the requests the command sent GitHub's API. A
`check --plan` result's plan carries `evaluated` for each target in each
environment where it's planned: its `target` and `environment`, and the
resolved `variants`, `depends_build`, `depends_lib`, `depends_run`,
`configure_args`, and `configure_env`, as the plan evaluated them; a recorded
plan, as a finished check's, has none. A check's `links` are lines, each with
its `mark` and `words`; a declaration nothing links carries `unlinked`, its
`port`, `option`, and `line`, and an undeclared link a declared port's
pkg-config brought in carries `through`, that `port` and the `modules`
followed. `tidy --json` lists a declaration's commit, `<port>: declare
libiconv`, among `commits` after the branch's, and `not_declared` says the
findings it leaves to you. A check's `portfile_findings` are the
Portfile findings submit would say, each with its `code`, `severity`, `where`,
and `message`, which the check said before building. Keep
standard error separate when parsing standard output. `-v` adds detail about
the work, and `-vv` adds individual steps. Long human lists can be shortened;
their JSON lists retain all entries.

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

`rebase --json` gives `branch`, `from`, `to`, `up_to_date`, `commits`, and,
once it moves the branch, `checkpoint`. `rebase --plan --json` adds `plan`,
true, and `replay`, each of the branch's commits in order with its `id`,
`subject`, and `outcome`: `replay`, `drop`, `conflict` (with its `paths`), or
`untried` after a conflict; `refused` says why the rebase wouldn't move the
branch, and `commits` is the count the replay would make.

`submit --json` and `ship --json` give the pull request under `pull_request`,
with `pending`: each step the submission still owes when it returns, as `step`,
`reason` and `detail`. The reasons are `failed` (with the error),
`awaits-answer`, `may-have-been-sent` (a reviewer request with no answer, never
sent again), `refused` (GitHub's words), and `unread`. A pending step that
failed or was refused fails the command; the next `submit` accounts for it.

The process exit codes are:

| Code | Meaning |
| --- | --- |
| 0 | The command completed successfully, including a successful no-op. |
| 1 | An error or refusal; also a batch where none of the requested work could complete. |
| 2 | A check failed. |
| 3 | Attention is needed: for example a held bump, an uncertain release, a partially successful batch, or a checksum preview finding a mismatch. |
| 4 | A check is still running: `wait --for` or `check --for` reached its bound before the check ended; the check continues. |
| 130 | The command was interrupted. |

`check --for 9m` queues a check and follows it for at most nine minutes; `wait
--for 9m` follows an existing one the same way. Both exit 4 while the check
continues, saying what it is doing, so a bounded tool call polls with one
command: `dockhand check --for 9m`, then `dockhand wait --for 9m` until the exit
code isn't 4; `run.state` in the JSON says queued or running. Neither runs the
check in its own process: with no serve, a background worker runs it.

`status --json` distinguishes a branch's newest finished check, `latest_check`,
from its `active_checks`. The first check can still be running while
`latest_check` is null. `check_standing` gives the verdict for the current
files: `not-checked`, `checking`, `stopped`, `passed`, `incomplete`, `older`,
`failed`, or `attention`. Use the reported branch and run names for the next
inspection. A port that was checked only with variants is named by its build,
as `jq +docs`, wherever the JSON names what to accept or what blocks
submission, an attention item's `next` among them: pass that name to
`--accept` as it is. An open pull request's `pull_request.merge` gives each port's
maintainers, with the `maintainers_basis` and `maintainers_as_of` they were
read at, whom the description mentions, who approved it, the ports still
`waiting`, and when MacPorts' maintainer timeout ends; `detail.merge` says the
same as lines. A run's `started_at` and `waited_seconds` say when a check
started and how long it waited for its slot.

To follow serve's work from a script or a screen reader, `dockhand console
--plain` prints status once and then each event as a line; `console` does the
same wherever its input or output is not a terminal. `status --watch` and
`status --plain` are refused, naming the console.

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

Replace those names with the ones the command returned. `dockhand logs check-42
jq` shows one port's log; `-p` chooses a branch there, as in every command, not
a port. A stopped check may need
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

Flags override the corresponding environment selections. These variables, with
the two caches after the table, are the complete catalog beyond the usual
`HOME`, `PATH`, and `XDG_CACHE_HOME`.

| Variable | What it selects |
| --- | --- |
| `MACPORTS_TREE` | Ports checkout, unless `--tree` is given. |
| `DOCKHAND_DB` | Database, unless `--db` is given; otherwise `~/.dockhand/dockhand.db`. |
| `DOCKHAND_CONFIG` | Configuration file; otherwise `~/.dockhand/config.toml`. |
| `GIT_BIN`, `PORT_TCLSH_BIN`, `TART_BIN`, `GO2PORT_BIN`, `CARGO2PORT_BIN`, `XCODES_BIN`, `GH_BIN`, `PATCH_BIN`, `DIFF_BIN` | The program, unless `--tool name=path` names it; otherwise `[tools]`, then PATH. |
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
| `DOCKHAND_ARCHIVE_CACHE` | packages.macports.org listing cache; otherwise `dockhand/archives` in the cache directory. |
| `DOCKHAND_BUILD_HISTORY_CACHE` | ports.macports.org build-history cache; otherwise `dockhand/builds` in the cache directory. |
| `DOCKHAND_GITHUB_CLIENT_ID` | OAuth application used by `setup github`. |
| `DOCKHAND_GITHUB_LOG` | File to append each command's GitHub request spending to. |
| `VISUAL`, `EDITOR` | Editor for Portfiles and pull-request descriptions. |

Tart, `go2port`,
`cargo2port`, `port-tclsh`, and `portindex` are found on `PATH`, then in
MacPorts' prefix, `/opt/local/bin`, so a shell whose `PATH` lacks it, such as
one over SSH, still builds; a refusal names both places, `go2port isn't on PATH
or in /opt/local/bin`. `--tool name=path`, its variable, or `[tools]` names one
ahead of that search, and one that names a program that isn't there is
refused: `GO2PORT_BIN: /opt/local/bin/go2port isn't executable`. None of them
reaches a build environment, which runs its own MacPorts.

`serve --install` keeps `DOCKHAND_CONFIG`, `DOCKHAND_UPSTREAM`,
`DOCKHAND_PULL_REQUESTS`, `DOCKHAND_INDEX_MIRROR`, `DOCKHAND_INDEX_CACHE`,
`DOCKHAND_READING_CACHE`, `DOCKHAND_GITHUB_CACHE`, `DOCKHAND_ARCHIVE_CACHE`,
`DOCKHAND_BUILD_HISTORY_CACHE`, `DOCKHAND_TART_HOME`, `TART_HOME`, and
`DOCKHAND_SSH_DIR` in the agent, with its PATH and explicit
tree and database selections, and each program named, as `--tool name=path`. It does not write a token into the agent
file. Read `dockhand usage configuration` for settings and
`dockhand usage unattended` for installing and stopping the service.

Host-read explanation diagnostics retain the product's ordinary model policy.
The follower-off comparison exists only in a separately tagged survey build;
no product command, configuration or environment variable selects it.

Survey measurement records use schema 2. The optional `Tolerance` journal
field names each context's default and follower-withheld verdicts; it is a
measurement, not a change to the product's assessment. Lossless observation
captures and their source/Base files are checksummed and compressed in the
survey record, so later analysis need not read the original installation.

## Survey-only follower measurement

From a source checkout, build the separate developer tool with:

```sh
go build -tags surveymeasure -o survey ./tools/survey
./survey -tree "$TREE" -prefix "$PREFIX" -sample "$PORTS" \
  -index-cache "$INDEX_CACHE" -db "$RECORD" -measure-follower
./survey -replay-tolerance "$RECORD"
```

`-measure-follower` requires `-db`, current mode and recording enabled. It
compares the same complete observation with the ordinary follower and with
follower approval withheld, and stops on a default-policy parity mismatch.
The ordinary assessment remains the journal's outcome. `Tolerance` entries
record both verdicts, whether each modelled context is left, and parity.
Replay opens the record read-only and prints JSON counts and compressed byte
totals. It uses captured source and Base bytes and runs no Tcl evaluations.
Only a tagged survey build supports either flag; the Dockhand CLI cannot
select the off policy. Resume requires the same binary, Base sources and
selection. Use a new record after rebuilding the tool.

In `Tolerance`, `On` and `Off` are true when inconclusive; `OnLeft` and
`OffLeft` respect the native-context exception. `Complete: false` identifies
an incomplete evaluation, not a classifier verdict or parity mismatch.

The run manifest's `tolerance` totals report complete parity checks, flips,
unavailable evaluations and missing captures. A measurement with no captures or
a worker failure exits unsuccessfully; its partial record remains diagnostic.
Do not call a partial run whole-corpus parity. Replay's `Changes` list includes
source mentions for grouping; these are witnesses to inspect, not a proof that
every mentioned line belongs to the accepted flow.

The manifest also sums elapsed seconds spent in paired analysis (including the
legacy parity check), capture preparation and JSON/compression. Parallel
analysis durations overlap; they are not an increase in survey wall time.
