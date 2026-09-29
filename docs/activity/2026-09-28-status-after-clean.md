# 2026-09-28: status after clean

The hugo exercise's "Cleaning up" ([review](../reviews/2026-09-28-hugo-bump-exercise.md#cleaning-up), findings 1 and 2). `dockhand clean --yes` removed eight merged branches: their worktrees, local branches, and fork branches. `status` then read the removal as trouble.

## What was wrong

1. **A cleaned branch needed you.** `status --all` listed each of the eight under Needs you: "its Git branch is gone", with `dockhand adopt <new name>, if you renamed it` as the way on. Its row said Work "branch gone", 0 ports, and PR "#35000 merged, not pushed". `status` read any missing Git branch as lost, whatever state the branch was in, though clean removes a merged one on purpose. It keeps only an event, not a mark on the record. And "not pushed" compared the pull request's pushed commit with a head that no longer exists.
2. **A cleaned branch couldn't be named.** `status hugo-9tu8` said "no tracked branch named hugo-9tu8", though `clean`'s help says the record stays and `status --all` lists it. Looking a branch up by name skips merged records, since a merged branch's name may be used again, and `status` looked up as every other command does.

## What changed

- **The engine says when a merged branch was cleaned** (`BranchStatus.Cleaned`): its Git branch is gone, and its work is in master. Any other branch whose Git branch is gone is still lost, as to a rename, and still needs you.
- **status shows a cleaned branch as that:**
  - nothing under Needs you, and no Next;
  - Work "cleaned", and "—" for its ports, which went with its Git branch;
  - its pull request as merged, since "not pushed" is said only of a branch that has a head;
  - in the branch's own view, no worktree, ports, or checks: only that it was cleaned after its merge, and its pull request.
- **JSON marks it** `cleaned`, and its `attention` leaves it out.
- **`status <branch>` finds a merged record** (`Engine.ResolveRecord`), the newest of that name, when no live branch has it. A live branch of the name comes first. Commands that change a branch still resolve only live ones, so `update --branch` of a merged branch still says there's none. The store gained `MergedBranchNamed`.

`clean`'s help and the guide say what status shows.

## Tests

`TestCleanAfterTheMerge` goes on past the clean:
- `status --all`, in text and JSON;
- the branch's own view;
- `update --branch`, refused;
- a new branch that takes the name, which `status` then finds before the merged record.

`TestBranchesKeepTheirRules` covers the store's lookup: the merged record rather than the open branch of the same name, the newest of two, and a name never used. The existing rename test still shows a lost open branch needing you.

Twelve mutations each fail a test. One was caught only once the test compared the pull request line: once a new branch took the merged branch's name, its Git branch was found again, and the two views were the same but for that line.

**Seen, not changed:** a merged record whose name a new branch took shows the new branch's Git branch in `status --all`: its commits and changes, as if they were its own. A merged branch's view could stop reading Git by its name. It's rare, and `status` has done it since merged records were kept.
