# Choosing a branch and keeping its work

Use this page when you need to find a change, start one without disturbing
another, bring existing Git work into dockhand, or finish a branch's life after
review. For initial setup, run `dockhand usage`. For editing the port itself,
run `dockhand usage hand-edits`.

A dockhand branch is an ordinary Git branch of the ports tree, together with a
record of its base on MacPorts' master, its checks, its edits, and at most one
pull request. It usually has a sparse worktree of its own. You can have several
changes in progress without switching your main checkout between them.

The examples below run in your `macports-ports` checkout. Names such as
`jq-update` and pull request number `35174` are examples; use the name or number
dockhand reports for your work. If you are working elsewhere, point dockhand at
the ports checkout with `--tree /path/to/macports-ports`. This selects the
repository; selecting a branch within it is a separate choice.

## Find the change before acting on it

Start with the overview, then inspect the branch you mean:

```sh
dockhand status
dockhand status -b jq-update
dockhand path jq-update
```

`status` groups work by what needs you, what is under way, and what is waiting
on review. A branch's detail includes its files and checks. `path` prints the
worktree directory, suitable for an editor or a shell:

```sh
cd "$(dockhand path jq-update)"
dockhand status
```

Inside that worktree, commands with no branch selector use its branch. Outside
it, the following selectors make your intent explicit:

| Selector | Meaning | Example |
| --- | --- | --- |
| `-b <name>` | The exact tracked branch. The `dockhand/` prefix is optional. | `dockhand diff -b jq-update` |
| `-p <port>` | The open branch changing that port. | `dockhand status -p jq` |
| `--pr <number>` | The tracked branch associated with that pull request. | `dockhand status --pr 35174` |

Choose one selector at a time. These selectors are available on commands about a
whole branch, including `check`, `tidy`, `submit`, `diff`, `rebase`, and
`archive`. The check-history commands also accept them:
`dockhand logs -b jq-update` finds that branch's latest check, while
`dockhand logs check-42 jq` names a particular check and port.

A selector overrides the branch checked out where you run the command. Dockhand
says so when they differ. `-p` can be ambiguous when more than one open branch
changes the port: at a terminal dockhand offers the choices; without a terminal
it refuses and names them. Use the exact `-b` name for a repeatable script or
when two changes overlap.

`--pr` on these commands selects an already tracked pull request. To bring a
pull request into dockhand for the first time, use `adopt --pr`, described
below.

## Start new work without touching another checkout

For a version update, the short path is:

```sh
dockhand update jq --new
```

Dockhand fetches master, prepares the edit, and starts a branch named for the
port and version when there is an edit to make. It reports the actual branch
name and directory. An already-current port starts no branch. To choose the name
yourself:

```sh
dockhand update jq --new=jq-update
```

`--new` starts work from master; it does not copy uncommitted work from the
checkout you are standing in. `-b jq-update` means “use this existing tracked
branch,” never “create it if missing.” A typo is refused rather than turned into
a new branch.

For a change you will write yourself, start the branch first:

```sh
dockhand start jq-fix --port jq
cd "$(dockhand path jq-fix)"
dockhand edit jq
```

`start` fetches master and creates `dockhand/jq-fix` in a worktree of its own.
`--port` can be repeated to bring several port directories into it. Without a
name, `start --port jq` generates one. `start jq-fix --here` instead creates the
branch in the current checkout, which must have no uncommitted changes to
tracked files.

`start --port` and `update` name each port's other open pull requests on
MacPorts' repository, such as `Also open for jq: #34620 jq: update to 1.8.0`,
so you can look before duplicating someone's work. They stop nothing, and a
search that fails says nothing; `submit` searches again before it publishes.

Without `--new`, an authoring command such as `update jq` uses the tracked
branch checked out here, or the one open branch already changing jq; if neither
exists, it can start one. Where several branches match, it asks or refuses as
above. `revbump` normally starts a separate rebuild branch unless you select an
existing branch or run in its worktree. Use `--new` or `-b` when this
distinction matters.

An update branch named for a version may be renamed when a later update moves it
to a different version, before it has a pull request. Its worktree can retain
its old directory name. Ask `path` for its location rather than constructing a
path from the branch name.

## Bring your existing Git work into dockhand

Use `adopt` when the branch already exists and should retain its work:

```sh
dockhand adopt my-jq-fix
dockhand status -b my-jq-fix
```

With no argument, `adopt` tracks the branch checked out here. It finds the base
where the branch leaves freshly fetched master; it does not rewrite your commits
or move the branch tip. If the branch is already checked out, dockhand records
that worktree. Otherwise it can place it in a managed worktree and reports where
it went.

This is the choice for changes made with Git before using dockhand. Creating a
new branch with `--new` would start from master instead of taking those changes
along. An authoring command that finds relevant changes on an untracked Git
branch points you at `adopt` rather than silently ignoring them.

For someone else's pull request:

```sh
dockhand adopt --pr 35174
dockhand status --pr 35174
```

Dockhand brings it into a branch such as `pr-35174`. Tracking it does not grant
permission to push to its author. Submission requires the pull request to allow
maintainer edits and your account to have the necessary access. Dockhand does
not rewrite their description. `dockhand usage review` covers inspecting and
checking adopted pull requests, including where their Portfiles are evaluated.

## Work in a sparse directory

A managed worktree contains `_resources` and the port directories needed for
that branch. A missing unrelated port directory usually means sparse checkout,
not lost repository contents. Bring another port into the branch with:

```sh
dockhand edit libharbor -b jq-fix
```

After tidy commits the work, a directory the branch does not change may be left
out again. Worktrees you created yourself retain their own width. Ordinary Git
tools and your editor still work in these directories. For installing a branch's
port with MacPorts, including how to avoid accidentally using master's
dependency, see `dockhand usage hand-edits`.

## Move onto newer master, or undo a history change

A branch does not need a rebase merely because master advanced. Rebase when its
work needs the newer base or when review requires it:

```sh
dockhand tidy -b jq-update
dockhand rebase -b jq-update
dockhand status -b jq-update
```

`rebase` requires committed work, fetches master, and replays the branch's
commits. A conflicting replay leaves the branch as it was and names the
conflicting files; there is no half-completed rebase for you to continue. A
branch containing merge commits needs Git work by hand. Inspect the reported
conflict and choose how to resolve the change before trying again.

Rebasing changes what the files build against. Follow status's next step,
commonly a new check; an earlier build may be reused where its inputs still
stand. Rebase does not push. The existing pull request keeps its old commits
until you submit again, and a rewritten push is conditional on the remote branch
not having changed.

Both tidy and rebase keep checkpoints. To undo the latest of those operations on
a branch:

```sh
dockhand undo -b jq-update
```

Or use the checkpoint name dockhand reported:

```sh
dockhand undo tidy-3
```

A named checkpoint already identifies its branch, so do not add `-b` to that
form. Undo requires the branch still to match the history the operation left;
later commits can make it refuse. After undoing tidy, your working files stay in
place and the edits become uncommitted again. After undoing rebase, files and
the base return together; intervening edits to files it would restore stop the
operation. Undo is not a general undo for updates, arbitrary Git commits, or a
push already made on GitHub.

## Set work aside, clean up after merge, or continue it

These operations have different purposes:

| Intent | Command | What remains |
| --- | --- | --- |
| Pause an open change | `dockhand archive jq-update` | Git branch, record, checks/checkpoints subject to retention, fork branch, and pull request. |
| Resume an archived change | `dockhand archive --undo jq-update` | The branch returns to open work; its worktree is available again when needed. |
| Remove merged worktrees and branches | `dockhand clean --plan`, then `dockhand clean` | The tracking record remains, so `status --all` can still explain the branch. |
| Forget an ended change entirely | `dockhand clean --delete jq-update` | The preview identifies the worktree, Git branch, unmerged work, and record to remove. |

Archiving hides the branch from the ordinary overview. It does not close a pull
request. With uncommitted edits or untracked files, the terminal offers how to
handle them; a nonterminal invocation keeps the worktree. `--keep-worktree`
makes that choice explicit. `--discard` removes the worktree and those files.
Ignored files in a removed worktree go with it too; they are not preserved by a
Git commit.

After a merge, refresh the observation and inspect cleanup:

```sh
dockhand status --refresh
dockhand clean --plan
dockhand clean
```

Clean keeps worktrees with edits or untracked files, branches that moved beyond
the merge, and a fork branch someone moved since dockhand last pushed it.
`clean --closed` also considers pull requests closed without merging, while
preserving local work master lacks. A named `clean jq-update` considers only
that ended branch; an open branch must first be finished or archived. Use
`status --all` to inspect the records left behind. `clean --delete` is the
explicit step that forgets an ended branch; its preview matters because unmerged
commits can go with it. In a nonterminal invocation, clean requires `--yes` to
apply its plan.

If you committed more work after a pull request merged, keep it as a follow-up
rather than deleting it:

```sh
dockhand start --from jq-update jq-followup
dockhand status -b jq-followup
```

This takes the commits beyond the recorded merge and replays them onto fresh
master in a new branch. A change already present on master is dropped; a
conflict starts nothing. Status points at this route when it finds work past a
merge. For the next check and submission, continue with
`dockhand usage publishing`.
