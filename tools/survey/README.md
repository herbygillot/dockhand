# Whole-tree survey

`tools/survey` assesses every port of a ports tree at its committed HEAD,
as v2's `dockhand assess --all` did, and compares two surveys. Each phase
of the oracle is proven this way ([scope](../../docs/oracle.md)).

```sh
go build -o /tmp/survey ./tools/survey

# Pin the tree: a detached worktree of the ports checkout at one commit.
git -C ~/src/macports-ports worktree add --detach /tmp/ports-<commit> <commit>

# Survey it. -prefix pins the MacPorts whose port-tclsh and portindex
# evaluate; the earlier baselines used the first on PATH.
/tmp/survey -tree /tmp/ports-<commit> -prefix /opt/macports-test \
  -journal ~/.dockhand/surveys/<date>-<name>.jsonl 2> <date>-<name>.log

# Compare with the survey before.
/tmp/survey -compare ~/.dockhand/surveys/<before>.jsonl ~/.dockhand/surveys/<after>.jsonl
```

The journal is one JSON line per port, the same as `assess --journal`
wrote, so older baselines compare directly. A rerun with the same journal
continues it, and it refuses a journal of another commit.

Beside the journal, `<journal>.run.json` records how the survey was made
and what it cost:

- the tree, commit, `port-tclsh`, `portindex`, and MacPorts version;
- the parallelism and the load average at the start;
- wall, user, and system time, including the evaluators, and the cores
  that implies;
- the outcomes.

Wall time depends on what else the Mac is doing, so compare costs only
between runs made under similar load.

Flags come before the journals: Go's flag parsing stops at the first
argument that isn't one, so `-compare -top 30 before.jsonl after.jsonl`.

`-ledger <file>` writes the dispatcher's ledger for each port and
evaluation context: every call the oracle's dispatcher passed, counted
by command, `file` subcommand, and where the answer came from.

## The record

`-db <file>` records facts rather than answers: every port in every
context its assessment observes it in, with what the evaluator read
along the way, in one SQLite file per snapshot of the tree
(`~/.dockhand/surveys/<date>-<commit>.db`, never dockhand's own
database). Each question is then a query, one per question in
[queries](queries); a comparison attaches the other record
(`ATTACH 'guest.db' AS guest`). The schema is [schema.sql](schema.sql).

```sh
/tmp/survey -tree /tmp/ports-<commit> -prefix /opt/macports-test \
  -db ~/.dockhand/surveys/<date>-<commit>.db 2> <date>-<commit>.log
sqlite3 ~/.dockhand/surveys/<date>-<commit>.db < tools/survey/queries/q01-files-read.sql
```

With `-db` the evaluator records (`eval.Evaluator.Recording`): the paths
`file`, `open`, `glob`, and `source` read, by what each is and who
answered, the command lines `exec` ran, the hooks the Portfile and its
PortGroups declared, each PortGroup's version and file, and once a run
what `mportinit` read and the aliases Base gives a worker. `-recording=false`
leaves those out, for measuring what they cost.

`-mode` is what each port is evaluated against, and is recorded on every
run:

- `current`, the default: the fresh installation, the facts table in
  modelled contexts, and the Mac's own in its own; the ports are assessed
  as before, and the journal is written beside the record
  (`<file>.jsonl`, with its `.run.json`).
- `installed`: the fresh installation with a registry that holds, for
  each port the current run saw ask the registry about another, those
  ports installed and active at the version, revision, epoch, and
  variants the current run evaluated them at. It surveys those ports
  alone, and doesn't assess them.
- `host`: the Mac's own installation, in shadow: reads and registry
  questions reach the host, and writes, the network, and unlisted
  programs are still refused. It is for a guest, over `-sample`.

`-sample <file>` names ports to survey, one a line. A rerun of the same
mode continues its run, skipping the ports it holds, and a record of
another commit is refused.

`-port`, `-category`, `-maintainer`, and `-not-maintainer` survey a
selection. `-parallel` sets how many Portfiles are assessed at once
(8 by default, as before). `-cpuprofile` profiles the survey itself.

The comparison covers the ports both journals hold:

- outcomes and the moves between them;
- regressions and improvements;
- ports whose findings changed, by pattern;
- fetch guards lost or changed;
- fetches refused then accepted;
- Portfiles fully covered;
- where the ports that stop, stop.
