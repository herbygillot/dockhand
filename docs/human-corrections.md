# Human corrections and publication

This is the agreed implementation direction. Ordinary Git edits, source capture, verification, and initial publication already exist. Managed amendment, rebase, reassociation, and existing-PR updates are now implemented through the shared workflow lifecycle.

## Ordinary Git is the primary editing surface

A contributor can switch to a Dockhand branch, edit Portfiles or patches, and use `git commit --amend`. No marker in Git config, commit trailers, private refs, or tags is required. `verify` captures tracked working-tree changes and staged new files; `verify --branch NAME` selects committed contents. GitHub verification requires committed contents and an explicit branch. Publication always selects committed contents.

A change ID scoped to the registered repository is the durable identity. The local branch name is its user-visible locator. Every accepted replacement source becomes a new immutable revision under that change. Old jobs and evidence remain historical observations. A rebase or amended commit does not invalidate evidence solely because the commit ID changed: applicability compares the complete source tree and accepted build inputs. If they differ, verification must run again.

Existing commands should report the selected branch, commit/tree, revision, evidence, and associated PR. They must not silently adopt a moving branch while executing an already accepted job. A newer revision supersedes publication authority for the older one; the driver checks the current revision before any remote effect.

## Managed amendment

Interface: `dockhand amend [--branch NAME] [--diff] [--publish] [--wait|--trace]`.

The default branch is the current tracked contribution. For this first implementation, stage the intended contents before adopting a checked-out amendment; Dockhand does not automatically stage edits. `--branch` selects committed contents. Amendment captures tracked edits and staged new files using the same capture boundary as verification, preserves the original contribution base, and constructs one replacement contribution commit. It preserves the title unless the user explicitly supplies a replacement. Changes outside the expected port directory or ambiguous untracked files require the user to resolve them first. It does not absorb unrelated commits or silently stage new files.

`--diff` shows the difference from the current recorded revision without moving refs or accepting work. An accepted amendment follows preparation, guarded branch replacement, verification, and optional publication. Without `--publish`, it stops after verification. It uses the existing state/claim lifecycle rather than spawning a separate controller.

Updating a checked-out branch requires a clean index/worktree after the captured changes and a recheck of their exact captured identities. A branch checked out in a different worktree must be handled explicitly rather than invalidating that worktree's index. Stage the replacement privately, then adopt it only when the branch and checkout preconditions still match. If they moved, preserve the candidate and report the conflict; do not reset or discard user work.

## Rebase and squash

Interface: `dockhand rebase [--branch NAME] [--diff] [--publish] [--wait|--trace]`.

Switch away from the contribution branch before running a managed rebase, including in linked worktrees. Fetch and freeze the intended upstream base. Reapply the contribution in a disposable Git workspace and retain one contribution commit relative to that base. The source, previous branch head, and intended base are recorded before replacement. A successful rebase produces a new revision and requires applicable verification before publication.

Conflicts stop the operation and preserve the disposable workspace with instructions for inspection or manual resolution. The user's checkout is not left in the middle of a rebase. A generic Git-operation journal in SQLite is unnecessary: recovery compares the original head, candidate head, and actual branch head. Original means adoption can retry; candidate means adoption happened and can be recorded; another head requires attention. An interrupted operation never overwrites the third case.

## Renamed or missing branches

A missing recorded branch is an actionable error, not evidence that the contribution was deleted or that a similarly named branch should replace it.

Interface: `dockhand reassociate CHANGE_ID --branch NEW_NAME`.

This changes the local branch locator after verifying repository identity, expected contribution scope, base, and current revision. Refuse a branch already owned by another open change. Matching source can retain applicable evidence; different source becomes a new revision and must be checked normally. Perform the metadata adoption transaction with the previously observed change/revision as preconditions. Active branch-mutating or publishing operations must settle or be canceled first.

The local locator and an existing PR's remote head branch are distinct. A local rename does not rename or recreate the remote PR. Once a PR exists, subsequent publications retain its recorded forge/repository/number and head branch unless a separate explicit migration is designed.

## Publishing corrections

Keep standalone `publish` explicit: it requires applicable passing verification and reports the exact `verify` command needed when evidence is missing. It neither starts an unexpected expensive build nor joins an unrelated active attempt. A combined `amend --publish` or `rebase --publish` already expresses authority to verify and publish that new revision.

Publication validates the current change/revision, local source, complete required coverage, authenticated destination, and expected remote head. Replacing a previously pushed contribution uses a conditional push against the observed remote head. Divergence requires attention; there is no unconditional force push. Recheck authority before each external effect, and reconcile ambiguous responses before retrying.

Update the associated PR rather than creating another one. Preserve human-edited PR text by default; do not regenerate the whole body from stale templates. A later implementation can replace a clearly bounded Dockhand verification section, with a content precondition so concurrent human edits are preserved. Closed or merged PRs require a new explicit decision, not automatic reopening or recreation.

Requested dependent coverage is part of verification authority. A passing root build cannot substitute for missing, failed, or incomplete downstream coverage. Discovery alone never authorizes edits to downstream Portfiles; automatic cohort revision bumps remain a separate explicit preparation choice.

## Boundaries and acceptance checks

- `git/changeset` captures source and constructs/rebases contribution commits using Git primitives; it does not decide port scope or workflow authority.
- `macports` evaluates expected targets and edit fidelity. `verify` judges evidence and coverage. `publish` owns destination and remote preconditions.
- `workflow` owns revision adoption, claims, supersession, and transitions. SQLite stores durable intent and results; external Git/forge work runs outside write transactions.
- CLI commands bind user intent and render results; they do not create a second progression loop.

Before shipping these commands, exercise edits during binding, a branch move during verification, a rename during publication, stale claims, index/worktree changes during adoption, rebase conflicts, a lost push response, concurrent PR text edits, and recovery after branch replacement but before state adoption. Confirm that same-tree amendments can reuse evidence and changed-tree amendments cannot. Confirm that a failed dependent blocks PR updates even when the root passed.

## First implementation boundaries

The implementation preserves the recorded contribution message unless `--title` supplies a replacement subject. It requires staged matching contents for a checked-out amendment and refuses a checkout-changing rebase or a branch occupied by another worktree. These conservative limits replace the proposed automatic checkout adoption until that can preserve both index and worktree intent across crashes. Rebase preparation happens before durable job acceptance; a conflict reports its retained worktree directly. Accepted candidates use the ordinary durable preparation/integration checkpoints.

GitHub verification continues to require exact commit/branch evidence; tree-only reuse applies to local verification. After a local branch rename, the local name only locates the commit: forge verification pushes to and observes the PR's recorded head branch, the same remote identity publication keeps, so one contribution has one fork branch and evidence is compared on it. No baseline build or causal inference is added for dependency failures.
