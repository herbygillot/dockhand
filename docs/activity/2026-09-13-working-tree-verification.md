# Working-tree verification — 2026-09-13

## Implemented behavior

`verify <port>` now captures working-tree contents. `verify <port> --branch <branch>` continues to select committed contents, including when the named branch is the current branch. Output and recorded status distinguish these inputs and show the accepted tree; working-tree provenance retains the branch or detached source, observed HEAD, and modified-file count. One explicit verification target is still required.

Capture includes tracked modifications, deletions, staged additions with subsequent unstaged edits, executable modes, and symlink targets. It uses raw bytes without clean/smudge filters, matching existing raw materialization semantics. Nonignored untracked files in the selected port, modified port directories, or shared resources require staging. Other untracked files are excluded. Sparse/skip-worktree entries, unresolved conflicts, submodules, unsupported file types, and files larger than 128 MiB are refused. An existing HEAD commit is required; detached HEAD is supported.

## Organization and identity

All mechanisms fit existing packages. `git/worktree.go` reads the index's tracked paths and raw files, hashes their Git blob representations, writes changed blobs, and assembles a tree through a private temporary index. SHA-1 and SHA-256 object formats work. Neither the real index nor refs are written. Two content reads plus repeated index/HEAD checks reject detected concurrent changes; this is not a filesystem-wide atomic snapshot, and it does not lock out ordinary editors. Once captured, the immutable tree is the sole source used by the accepted verification run.

`workflow.BindVerification` chooses capture for an empty branch and shares snapshot materialization and evaluation with explicit branch binding. Tracked contribution revision association remains intact, including tree-only revisions, while standalone or detached verification creates no contribution. `record.Checkout` stores provenance on accepted job options. Dirty captures have an empty source commit; their observed HEAD remains separate from `Source.Base` and never falsely identifies the edited tree as committed. Clean captures retain their matching commit.

SQLite reads and writes the optional provenance through existing `jobs.options` JSON. No schema migration, new table, dependency, package, global lock, hidden ref, or synthetic commit is needed. `verify.PlanSingle` and Tart consume a mandatory tree and optional commit. Tart checks commit/tree agreement when present and materializes the accepted tree before creating its guest archive. Admission, recovery, cancellation, and resource cleanup use the existing machinery. Missing Git objects remain a visible consumption failure.

Source capture is proportional to the checkout size and happens during binding, not on every driver cycle. Evidence reuse after committing a tested tree, inferred targets, and publication remain later work. Bump preparation still selects committed source.

## Validation

New Git tests cover raw-byte capture despite configured clean filters, staged/unstaged precedence, both kinds of deletion, staged additions, ignored/untracked exclusion, executable bits, symlinks, assume-unchanged flags, index/ref preservation, detached and linked worktrees, conflicts, sparse entries, concurrent modification, and SHA-256 objects. An opt-in real ports checkout test checks capture duration and preserves HEAD and index bytes.

Workflow tests exercise standalone and tracked dirty snapshots, submission after subsequent edits, idempotent retries, database reopening, shared verification planning, explicit branch selection, relevant untracked files, staged patches, and detached HEAD. CLI tests use native MacPorts and a fixture image descriptor, detach immediately after acceptance, and check the stored source and output for default versus explicit-branch selection. Tart tests verify tree-only guest archive contents and admission, optional commit/tree validation, and missing-object failure. No live VM build is claimed by these tests.

`make test-race`, `make vet`, `git diff --check`, and `make build BINARY=/private/tmp/dockhand2-worktree` passed. Native MacPorts tests ran. The temporary binary's verification help reflects the new selection behavior; the user's existing `dh2` executable was preserved.

The real `~/Source/macports-ports` checkout capture passed in 12.05 seconds, with zero modified files. It retained HEAD `11f22962ec196b85d04fd400eb13d7c1751b5c47` and produced tree `561db7774a42dc87255b714b3950786b263cf972`; the test confirmed that HEAD and all index bytes remained unchanged. This is a single local timing, not a performance guarantee. Full-tree capture cost is paid for each fresh checkout binding; reattachment and driver cycles reuse accepted objects.

## Provenance

All new code and tests were authored for v2. Existing v2 snapshot binding, state options, planning, and Tart input mechanics were extended. No v1 code, comments, or tests were copied. No new dependencies were added.
