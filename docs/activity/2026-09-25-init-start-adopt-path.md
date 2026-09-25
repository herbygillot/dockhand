# 2026-09-25: `init`, `start`, `adopt`, and `path`

Roadmap step 5, part 3: v3's first working commands (Design v3 §4, §6.1–6.2, §6.9).

## The commands

- **`dockhand init`**
  - Registers the ports checkout and finds its remote for macports/macports-ports by URL, whatever the remote is called.
  - Chooses where branch worktrees go. The default is `macports-branches` beside the clone, such as `~/src/macports-branches`. `--worktrees` or a terminal answer records a different directory in `~/.dockhand/config.toml`; the default needs no file.
  - Reports whether MacPorts' `port-tclsh` is available, and where the records are kept.
  - Needs no GitHub login and no build setup. It is safe to rerun.
- **`dockhand start <name>`**
  - Fetches MacPorts' master fresh and creates `dockhand/<name>` there, in a sparse worktree holding `_resources`, which grows as ports are edited.
  - `--here` creates the branch in the current checkout instead, and refuses while tracked files have uncommitted changes, listing them.
  - Refuses a name already tracked, a Git branch that already exists (pointing to `adopt`), and an occupied directory.
  - A failure partway undoes what it did, removing the worktree and deleting the branch, so a failed start leaves nothing behind.
- **`dockhand adopt [branch]`**
  - Tracks the checked-out branch, or the one named, as it stands; nothing is moved or rewritten.
  - Reports the commits above master and the ports it changes, by MacPorts CI's rule: a `Portfile` or `files/` change in `<category>/<port>`. It mentions `_resources` separately.
  - Its base is where it leaves freshly fetched master. `master`, `main`, and a detached HEAD are refused with the command that fits.
- **`dockhand path [branch]`**
  - Prints only the directory, for `cd "$(dockhand path jq-update)"`.
  - Without a name, it prints the branch checked out here.
  - A branch not checked out, or a worktree that has gone, is an error saying so.

## New packages and additions

- **`internal/engine`** holds the operations, bound to a ports checkout, Git, and the store. It includes v2's ports-tree check, ported from `v2-final:internal/app/repository.go`: a directory with no `<category>/<port>/Portfile` in its working tree or on a local branch is refused before anything is recorded. The check also accepts a sparse worktree. It also includes `ScopeOf`, CI's changed-directory rule, and name resolution with or without the `dockhand/` prefix.
- **`internal/config`** reads `~/.dockhand/config.toml` (decision 15; `DOCKHAND_CONFIG` names another file). It knows one key so far, `worktrees`, and refuses unknown keys by name. Reading creates nothing, and writing a key keeps the rest of the file.
- **`internal/git`** gains `CreateBranch` (which refuses an existing branch), `DeleteBranch` (only while the branch still points where expected), `AddSparseWorktree`, `ExpandSparse`, `SparseCone`, `RemoveWorktree`, `TrackedChanges`, and `Switch`.
  - A `--no-checkout` worktree has an empty index, so setting sparse patterns alone populates nothing. `AddSparseWorktree` follows the patterns with `read-tree -mu HEAD`, which fills exactly the cone.
  - Git turns on `extensions.worktreeConfig` in the repository to keep the patterns with the new worktree. The person's own checkout keeps every file.
- **`internal/command`** gains the four commands and the global `--tree`/`-t`, `--db`, and `--git`, each following flag, then environment (`MACPORTS_TREE`, `DOCKHAND_DB`, `GIT_BIN`), then default. `DOCKHAND_UPSTREAM` fetches master from a mirror or, in tests, a local repository.

## Found while running it

- **`/dev/null` counted as a terminal.** Terminal detection first checked for a character device, which `/dev/null` also is, so `dockhand init </dev/null` asked a question and failed on end of input. It now asks the terminal driver (`TCGETS` on Linux, `TIOCGETA` on macOS, through `x/sys`). End of input at a prompt takes the default.
- **`init` named the wrong fetch source.** It named GitHub even when `DOCKHAND_UPSTREAM` pointed elsewhere; it now names the URL actually used.
- **Unformatted test files.** Two test files were not gofmt-formatted, one of them committed with the store (`3ab1a50`). CI runs no formatting check, so both are now formatted and `make fmt-check` is added, run by CI, to name any unformatted Go file outside `vendor`.

## Tests

The engine and command tests run against a local upstream standing in for macports/macports-ports and a clone of it, with upstream moved on after the clone, so a branch provably starts from fresh master rather than the clone's stale one. They cover every refusal above, cleanup after a failed worktree and after a failed fetch, `--here` with tracked and untracked changes, adoption with the CI scope rule, resolution from inside a sparse worktree, remote detection by URL form, and the configuration file's rules. `go test ./...`, the race runs, `go vet` (Linux and `GOOS=darwin`), `make vendor-check`, and `make fmt-check` pass.

## Still to come in this part of the design

- `adopt --pr`, which needs the GitHub layer.
- Reconciling a branch renamed with Git.
- `--json` on these commands.
- The capability report's other lines: your ports, providers, and publishing.
