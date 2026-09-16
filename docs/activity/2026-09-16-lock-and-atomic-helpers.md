# Shared lock naming, lock scopes, and atomic replacement

Implements the utility items from the [structural review](../reviews/2026-09-16-bump-machinery-structure.md) that removed real repetition. No new package: `filelock` and `atomicfile` gained the forms the review asked for, and the hand-written copies were routed through them.

## filelock

`Path(directory, key)` names a lock file by the SHA-256 of its key, which is the scheme every caller already used with its own digest helper; the produced names are byte-identical, so existing lock files keep their inodes. `With(ctx, path, mode, fn)` holds a lock around a callback and hands the callback the file so a child process can inherit the descriptor. `Acquire` now opens with `O_NOFOLLOW`, matching `TryExisting` and the removed git implementation.

`git.withLock` was a second flock implementation with its own 25 ms polling loop; it is now three lines over `filelock.With`, keeping only the context value that lets git children inherit the lock. The GitHub provider, its log retention, and the Tart submission lock name their files through `Path`. The PortIndex generation lock no longer falls into a blocking acquire on a non-busy error such as a canceled context; busy and absent locks acquire, anything else returns.

## atomicfile

`Create(path, mode, write)` replaces a file with contents a callback writes to a synced sibling and syncs the parent; `Write` is now a thin wrapper over it. `ReplaceDirectory(destination, build)` builds a directory in a temporary sibling, retires any previous directory only after the replacement is in place, and restores it if the final rename fails, so a crash leaves either the old or the new directory.

Routed through them: the PortIndex `latest` pointer and environment description (previously a near-duplicate of `Write` without syncs), PortIndex generation publication (previously removed the old generation before renaming the new one in), the verification staging archive, GitHub log downloads, and Tart guest log capture. The six temp-then-rename sites now agree on syncing the file and its parent.

## Left as is

The review's shared exec helper and poll helper were not done in this pass. The exec helper touches git's environment blocklist, which is a security boundary, and each caller's wait-delay policy; it deserves its own change with the environment policies kept per tool. `TryExisting` is left as the maintenance-side primitive: its callers need to distinguish busy from absent, which a try-or-skip form would conflate.

## Checks

- New tests: `Path` stability and naming, `With` holding the lock and keeping the file, `Acquire` refusing symlinked lock files, `Create` preserving the destination on failure and leaving no temporary, and `ReplaceDirectory` retiring the previous directory only after success and preserving it on a failed build.
- `go test ./... -count=1` and `go vet ./...`: passed.
