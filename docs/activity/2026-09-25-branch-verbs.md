# 2026-09-25: `edit`, `revbump`, `retry`, `rebase`, and `archive`

The rest of Design v3 §5's verbs for working on a branch. None of them needs a Mac.

## The commands

- **`dockhand edit <port>`** finds the port's directory and adds it to a sparse worktree's cone. It then opens the Portfile in `$VISUAL` or `$EDITOR`. Without a terminal or an editor, it prints the Portfile's path.
  - A plain name is found as `<category>/<name>` in the branch's tree. Only a subport needs the port index.
  - The branch is chosen as for `update`: `--branch`, the one checked out here, `--new`, or a prompt.
- **`dockhand revbump <port>... --subject "<reason>"`** raises each port's revision through the same preparation path as `update`.
  - It has the same capture, the same guarded write-back, and the same `--plan`.
  - The reason is required. It is recorded as each commit's subject, `<port>: <reason>`, so a revision bump alone tidies unambiguously under that subject.
  - Without `--branch` or a branch checked out here, it starts a new branch without asking, as §6.6 says, since a rebuild has its own reason.
- **`dockhand retry <run>`** queues a finished check's exact request again: the same revision and plan, whatever the branch holds now. It then runs it here or hands it to `serve`, as `check` does. The whole plan is built again until per-target reuse lands (step 9).
- **`dockhand rebase`** fetches master and replays the branch's commits on it, in the branch's worktree.
  - The old history is kept as a checkpoint, `rebase-N`, that `restore` brings back. Checkpoints now have kinds and share one numbering.
  - The branch's recorded base moves with it.
  - Uncommitted edits are refused.
  - A conflicting rebase is aborted, leaving the branch as it was, and the conflicting files are named.
- **`dockhand archive [branch]`** hides a branch from `status` without touching its files, Git branch, or pull request. `status --all` still shows it, and `--undo` brings it back.

## Other changes

- **Schema 4:**
  - Rebuilds `edits` so the kind can be `revbump`: a CHECK constraint can't be altered, and a test upgrades a database with an edit in it.
  - Adds a checkpoint kind, where older checkpoints were tidy's.
  - Adds the pull request's observed state, as JSON, for following PRs next.
- `check`'s "run here, or hand to serve" became `runQueued`, which `retry` shares.

## Tests

- **Engine tests:**
  - `edit` growing a sparse worktree, and refusing an unknown port;
  - `revbump` refusing no reason, bumping, and tidying unambiguously under its subject;
  - `rebase` up to date, onto a moved master and back with `restore`, and a conflict leaving everything as it was;
  - uncommitted edits refused;
  - `retry` refusing a run still queued and repeating the pinned request;
  - `archive` and back.
- **A command test** takes one world through `revbump` starting a branch, `edit` in a sparse worktree, a check and its `retry`, `rebase` refused then up to date then onto a moved master, and `archive` and `--undo`.
- **Store tests** cover the schema-3 upgrade keeping edits, checkpoint kinds, and the pull request observation.
