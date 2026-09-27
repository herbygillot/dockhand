# 2026-09-27: history changes as complete transitions

Roadmap item 3. The [architecture and data-flow review](../reviews/2026-09-27-architecture-and-data-flow.md)'s finding 3: `tidy`, `rebase`, and `restore` changed Git inside database transactions, and a process that stopped between the two left a state nothing finished.

## Where this departs from the roadmap

The roadmap asked for a per-branch lock, Git first and then the record, and reading an uncertain commit back, and said a durable operation record was heavier than needed. It also asked that a crash between the Git change and the record leave a state the next command recognizes and finishes.

Those two can't both hold. If only Git changes before the record, a stopped tidy leaves a moved branch and checkpoint refs with no record of which branch, which heads, or which bases. The next command can't tell that from the person's own `git reset`, and can't restore it.

So the checkpoint is recorded first, as *prepared*, and settled after the Git change. That makes it the operation record the review asked for, with no new table: one state column. It also gives a checkpoint its number without Git inside a transaction, which was the only reason `ApplyTidy` moved refs inside one.

## What changed

- **One lock per branch.** `tidy`'s apply, `rebase`, and `restore` hold `git.WithBranchLock` throughout: a file lock in the Git common directory, which v2 had and v3 hadn't used. The operating system releases it when its holder ends, and the Git commands it runs inherit it, so a stopped dockhand can't release it while its Git command still runs. Another history change on the branch waits for it.
- **Checkpoints have a state** (schema 14):
  - *prepared*, recorded before the Git change;
  - *applied*, once made;
  - *abandoned*, when it wasn't.

  Checkpoints recorded before this are applied. `SettleCheckpoint` moves a prepared one once, and `MarkRestored` takes only an applied one.
- **Tidy** composes its commits, records its checkpoint as prepared, then moves the branch and the checkpoint's refs in one Git ref transaction. It then resets the index and settles the checkpoint. When the refs can't move, it abandons the checkpoint and changes nothing. It used to move refs inside the store's transaction and undo them on any error.
- **Rebase replays before anything moves.** `git.Replay` does what `git rebase` does to the branch's commits without a checkout or a ref:
  - each commit's change is merged onto the replayed commit before it, with `git merge-tree --write-tree --merge-base`;
  - it is committed with its own author, date, and message;
  - a commit master already has is dropped, and one empty to begin with is kept;
  - a conflict names its files and leaves nothing moved, as before;
  - a merge commit is refused, and rebased by hand.

  Then the rebase records its checkpoint with the head it will write, creates the checkpoint's ref, and moves the branch and its checkout together (`MoveCheckout`, `git reset --keep`). Its base moves in the transaction that settles the checkpoint.

  `git rebase` in the worktree moved the branch itself, so the rebased head wasn't known until Git had moved, and a stopped rebase couldn't be told from anything else. Replaying needs Git 2.40, and says so when it's older; Apple's is 2.54.
- **Restore** makes its Git change under the lock, then records the restore and the base in one transaction.
- **An uncertain commit is read back.**
  - A prepared checkpoint whose commit is uncertain is read back. If it landed, the change goes on; if not, nothing was changed, and the change stops there.
  - A settling or restore whose commit is uncertain and landed is done.
  - One that didn't land leaves the change made in Git, and says the next history change finishes the record. A Git change is never undone.
- **A stopped process is finished by the next one** (`settleHistory`, at the start of every history change on the branch, under its lock). From what Git shows:
  - a prepared checkpoint whose branch is at or past its new head is applied. A tidy's index is reset if it still holds what the tidy replaced, and a rebase's base moves;
  - one whose change wasn't made is abandoned, and the refs it made are removed where they are still as it made them. `restore` of it says "tidy-1 was never made";
  - the branch's newest applied checkpoint, not restored, whose branch is back at its old head, is recorded as restored, and a rebase's base goes back. That covers a restore that stopped before recording, or the same change made by hand. The index is left as it is there, since a person's `git reset --soft` looks the same.

  Each writes an event: "recorded tidy-1, whose change a stopped dockhand had made", or "abandoned tidy-1, ...".
- **Tests stop it** through a test-only step hook on the engine (`stopAt`), at "prepared" and "moved". That is where a process ending leaves each state; the next engine opened on the same database finishes it. A store wrapper makes one kind of commit uncertain, landed or not.

Tests:
- `Replay` against `git rebase` on the same commits: the same tree, authors and dates kept, a change master has dropped, an empty commit kept, a conflict named, a merge refused;
- a tidy and a rebase stopped before their Git change, abandoned by the next, their refs gone and their numbers not reused;
- a tidy stopped after its Git change and before resetting the index, recorded and restorable after; a rebase stopped after, recorded with its base by the next rebase;
- a restore of a rebase stopped after its Git change, recorded with its base put back;
- an uncertain commit of a prepared checkpoint and of its settling, landed and not;
- a tidy that waits for the branch's lock, and changes nothing when it gives up;
- the store's checkpoint states: settling once, and restoring only an applied checkpoint;
- the recovery tests fail with the recovery step disabled, checked by hand once.

## init checks for Git 2.40

The person asked that setup check for the Git rebase now needs. `dockhand init` is dockhand's setup; `providers setup tart` makes images and never runs the host's Git.

- **`git.ExecutableVersion`** runs the Git dockhand runs, and reads what `git version` says, whoever built it: "2.54.0 (Apple Git-157)", "2.40.0.rc1", "2.45.2.windows.1". **`git.MinimumVersion`** is 2.40, and `Replay`'s error names it.
- **`engine.GitVersion`** refuses an older one, naming its path and saying how to get another: "dockhand needs Git 2.40 or newer, and /usr/bin/git is 2.39.5; install a newer one, such as with: sudo port install git, or name one with GIT_BIN". The command layer may not import `git`, so the decision is the engine's.
- **`init`** checks it before anything is recorded, and lists it first: "Git ✓ 2.54.0 at /opt/local/bin/git". `init --help`, the usage guide, and the README say so.

Tests: versions as several builds of Git report them, compared with the minimum; `init` showing its Git; and `init` refusing a Git that says 2.39.5, with nothing recorded.
