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
