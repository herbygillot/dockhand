# 2026-09-22: the shared-file lookup runs before the transaction

The second part of Next item 1. The [shared-files note](2026-09-21-shared-files-note.md)
records, on every revision, the files under `_resources` it changes beside
its port and what loads them. The lookup runs `git diff-tree` against the
immutable source and, for a change under `_resources`, a tree-wide
`git grep`; and it was called where the revision is written, inside the
`BEGIN IMMEDIATE` transaction, at submission, at branch integration, and
at reassociation. The second architecture review measured what that
meant: the shared database writer held while subprocesses ran, delaying
every other job and repository on the same database.

Each site now reads the shared files before its transaction, from the
source the transaction then reconfirms, and hands the result in:
submission fills the branch input's `Shared` after normalizing the spec,
integration reads the candidate's after the branch is integrated and
before the result is recorded, and reassociation reads the branch's
before taking the branch lock. The helper's comment says what it costs
and where it belongs. The adoption tests that record shared files pass
unchanged, since the value is the same; what moved is when it is read.
