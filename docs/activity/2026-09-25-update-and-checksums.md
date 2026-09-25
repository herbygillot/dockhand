# 2026-09-25: `update` and `checksums`

Roadmap step 5, part 4: the first authoring commands (Design v3 §4, §5, §6.2, §6.5). They edit a branch's working files through v2's preparation code and commit nothing.

## The commands

- **`dockhand update <port> [version]`** moves a port to the newest upstream release, or to the version named, and writes its checksums. `--keep-old-checksums` and `--shared-release` carry over from v2.
- **`dockhand checksums <port>`** writes the checksums for the version the Portfile already names, which is the step after a hand edit of `version`.
- **Choosing the branch** (Design v3 §4):
  - `--branch <name>` picks a tracked branch; the `dockhand/` prefix is optional.
  - Otherwise the branch checked out here is used.
  - `--new` starts a branch in its own worktree. It can't be combined with `--branch`.
  - With none of these, and this checkout on `master` or no branch, a terminal is asked. When no open branch changes the port, it offers to start one (`[Y/n]`). When one branch does, it offers that branch or a new one (`[t]here / [n]ew / [q]uit`). A script gets an error naming `--branch` and `--new`.
  - A branch someone made and dockhand doesn't track is refused, with `adopt` named, rather than worked on silently.
- **Branch names.** A branch started without a name is `dockhand/<port>-<short ID>`, such as `dockhand/jq-4k2p`, per decision 37 (no verb or version, and never renamed). Design v3's journey 6.5 showed `croc-checksums`, which contradicted decision 37; it now shows `croc-7hq2`.
- **`--plan`** prepares the edit and prints it as a diff, changing nothing. Because a plan changes nothing, `--plan` with `--new` is refused rather than starting a branch.
- **Output.** The first line names the branch and directory. Then the version change and where it came from (`jq: 1.7.1 → 1.8.1   (GitHub tag jq-1.8.1)`), what was updated, the changed files, and any patch that no longer applies. A port already current says so and writes nothing.
- **After a successful edit**, output ends with "Next: review it with git diff, then commit it". Journey 6.2's `Next: dockhand check` waits until `check` exists.

## How an edit reaches the working files

1. The worktree's tracked files, as they are on disk, are captured into a Git tree: HEAD's tree with every staged or unstaged change applied (`git.WorkingTree`). An uncommitted edit is prepared from as it stands. Untracked files and files outside the sparse cone are not read.
2. Preparation runs on that tree, with the branch's base as the upstream base. It writes Git objects only.
3. For a sparse worktree, the cone grows to hold the edited port's directory.
4. Every edited file is checked against the contents and executable bit it had when captured, and only then is any written (`git.ApplyToWorkingFiles`). A file changed during preparation stops the edit with nothing written and the person's change intact.
5. A `branch.edit` event records what changed.

The real preparer is assembled on first use from v2's parts, as `v2-final:internal/app` composed them:
- the native evaluator (`port-tclsh` on PATH or in `/opt/local/bin`);
- the port index staged per source tree (`DOCKHAND_INDEX_CACHE`, else the user cache directory);
- upstream discovery on GitHub and GitLab;
- the GitHub login from the system keychain, whose key now lives in `internal/github` as `CredentialKey`.

## Changes by package

- **`internal/preparation`** is `internal/workflow/preparation`, moved so v3's engine can import it while v2's workflow still does (`b8a0994`).
- **`internal/git`**:
  - `WorkingTree`, `ApplyToWorkingFiles`, and `BlobID` are new.
  - `TrackedChanges` now lists both paths of a rename. It previously dropped the source path, so a capture would have kept a renamed file in its old place too.
- **`internal/engine`**:
  - `Update` is new, with a `Preparer` interface that tests fill with a stand-in.
  - `BranchesChanging` finds the open branches whose commits change a port. It matches by port directory name, so a subport of another directory isn't matched yet.
  - `FreeName` picks a decision-37 name.
- **`internal/command`**:
  - `update` and `checksums`, in an "Author" help group.
  - The configured `port-tclsh` is passed to the engine.
  - Tests can stand in for MacPorts and for a terminal.

## Tests

- **Engine tests** run `update` and `checksums` against a local upstream with a stand-in preparer that rewrites the version line. They cover:
  - the edit landing in a sparse worktree whose cone didn't hold the port;
  - an uncommitted edit carried through;
  - `--plan` changing nothing;
  - an already-current port;
  - a file changed during preparation, with nothing written;
  - a branch no longer checked out;
  - finding branches by port;
  - free names.
- **Command tests** cover:
  - context from the worktree;
  - both terminal prompts;
  - the script errors;
  - `--new`, `--branch`, and their conflict;
  - an untracked branch.
- **Git tests** cover capture (staged, unstaged, deleted, and executable files, with the index left alone) and the check-everything-first write.
- `go test ./...`, the race runs, `go vet` (Linux and `GOOS=darwin`), `make vendor-check`, and `make fmt-check` pass.

**Not proven here:** a real update. This container has no MacPorts, so `dockhand update <port>` against a real ports tree needs a run on a Mac.

## Still to come in this part of the design

- The stealth-update report: old and new checksums, the archive's changed files, and `dist_subdir ${name}/${version}_1` (§6.5).
- When a port can't be updated by itself, the explanation and the `edit` then `checksums` path (§6.3). For now preparation's error is shown as it is.
- `revbump`, `create`, and `edit`.
- `update --submit`, `--revbump-dependents`, and `update --outdated`.
- `--json`.
