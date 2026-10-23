# The Portfile corpus

`tools/corpus` replays MacPorts' own version bumps through dockhand's
`update`. It runs them against a pinned slice of macports-ports in
`testdata/corpus`, so update's plan path meets the real PortGroups instead
of the tests' stand-ins for them.

```sh
make test-corpus                      # build this checkout and replay every row

go build -o /tmp/corpus ./tools/corpus
/tmp/corpus replay -dockhand /tmp/dockhand -port opkssh   # one row
/tmp/corpus refresh -ports ~/src/macports-ports -commit <commit>
```

## What the slice holds

- `rows.tsv`: one exemplar per line. Each row gives the port, its
  directory, the MacPorts commit that bumped it, the version it bumped to,
  and what a replay does: `plans`, or `refuses: <words of the refusal>`.
  It also says why the port is in the corpus.
- `tree/`: `_resources` at `COMMIT`, with each row's directory as it was
  before its bump.
- `bumped/`: the Portfile each bump made.
- `LICENSE`: MacPorts' license, which the slice is under.

`refresh` rewrites `tree/`, `bumped/`, `LICENSE` and `COMMIT` from a ports
checkout that has the rows' commits. Review the result as a diff: that's
where a PortGroup's change upstream shows up, before it reaches the field.

## What a replay checks

For each row, `replay` runs `dockhand update <port> <version> --new
--plan` in a git repository made of the slice, with a HOME and database of
its own. Then it applies the plan's diff and compares the result with the
bump's Portfile. It compares the version lines, the checksums' sums and
sizes, and the dependency blocks (`go.vendors`, `cargo.crates`, and the
like) as words, where order doesn't matter. A row that should refuse
passes only when it refuses with its words.

It needs the network: the forge's tags, both versions' archives, and
go2port's and cargo2port's lookups. One run took about 2 minutes on the
main Mac.

A port named by a field or acceptance finding joins the corpus in the
commit that fixes it.
