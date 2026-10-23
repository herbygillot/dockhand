# 2026-10-23: The Portfile corpus, its Go and Rust half

The roadmap's "A corpus of real Portfiles replays update's plan path"
([proposal](../reviews/2026-10-23-plan-corpus.md)), placed before rc5's tag
at the person's "Before rc5" (12:26Z). Its time is recorded, with no
warm-cache target (prioritization, 2373b7f6).

## What it is

- **testdata/corpus**: a slice of macports-ports at `COMMIT` (ee76ef2c):
  `_resources` whole (1.4 MB, every PortGroup), MacPorts' `LICENSE`, and
  12 exemplar ports. Each port is kept as it was before its last version
  bump (`tree/`), beside the Portfile that bump made (`bumped/`).
  `rows.tsv` gives each row's port, its bump commit, the version, the
  expected outcome, and why it's in the corpus.
- **tools/corpus refresh** rewrites the slice from a ports checkout.
- **tools/corpus replay** runs each row's `update <port> <version> --new
  --plan` with a dockhand binary, through the command's own path. It runs
  in a git repository made of the slice, with a HOME and database of its
  own, so no entry point exists for tests alone. It applies the plan's diff
  and compares the version lines, the checksums' sums and sizes, and the
  dependency blocks with the bump's, as words where order doesn't matter. A
  `refuses:` row passes only when it refuses with its words.
- **make test-corpus** builds this checkout and replays every row. It needs
  the network.

## The rows

- **Go:** git-credential-oauth (rows with their own sums and sizes),
  direnv (/v2), opkssh (about 500 lines, the longest that plans), mailpit,
  go-reflex (small, dockhand's own bump), and two refusals. caddy is
  refused because its patch edits its manifests, the designed rule.
  terraform-ls is refused below.
- **Rust:** dust (rc1's dist_subdir), pgdog (git crates pinned by rev), hex
  (small), mise (the longest cargo.crates), and jaq (a workspace). The last
  three were dockhand's own bumps.
- helm-ls, the first choice for a long plan, has no bump commit; it's only
  a new port.

## Results

- rc4 (4aff01ca): 5 of 12, in 50 s. Every Go row fails: five on "the
  checksums are set by the golang 1.0 PortGroup", and three (opkssh,
  terraform-ls, caddy) on the RPC frame limit, caddy's at 54.8 MB.
- main: 12 of 12, in 1 min 52 s.

## Found

**terraform-ls** 0.39.0's replay refuses, "missing generated Go checksum".
Before regenerating, update checks the Portfile's go.vendors against
go2port's output for the current version, 0.38.8. That version requires
github.com/chigopher/pathlib, whose repository is gone (go2port: "HTTP
status=404"), so go2port writes it a sha256 of 0. The refusal is right,
because the block can't be checked, but its words don't name the module.
It's a row of its own and went to prioritization.

The comparison's first draft took a bump's `${distname}${extract.suffix}`
for a different file than the replay's `direnv-2.37.1.tar.gz`. A checksums
command is now compared by its sums and sizes alone.

## Git through internal/git

internal/git's boundary test says only internal/git runs git, and the tool
first ran it itself. Now `refresh` reads the ports checkout with git.Open,
`CommitTrees`, `MaterializeInto` and `ReadFiles`. `replay` makes its slice
a repository with a new `git.Init`, then `AddAll`, `IndexTree`,
`WriteCommit` and `CreateBranch`. It applies each plan's diff with
`patch -o` to a copy of the Portfile, which leaves the slice as it was. A
refresh on internal/git wrote the same slice as before.
