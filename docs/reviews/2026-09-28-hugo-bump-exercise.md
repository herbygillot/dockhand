# hugo bump exercise

Exercised 2026-09-28 against `~/Source/macports-ports`, taking `hugo` from 0.166.0 to 0.167.0 through to [macports/macports-ports#35000](https://github.com/macports/macports-ports/pull/35000). Most of the run used a build of `14320eb7`; the final tidy and the submit used `9f1f3ee8` (origin/main), for the reason under Friction 1.

## The run

| Step | Command | Time | Result |
| --- | --- | --- | --- |
| Find | `outdated hugo` | 12.5s | 0.166.0 → 0.167.0, "update" |
| Plan | `update hugo --plan --new` | 6.6s | Exact diff, eight upstream `go.mod` moves |
| Update | `update hugo --new` | 10.2s | Branch `hugo-9tu8`, version and checksums |
| Check | `check` | 3m14s | Passed on Tart macOS 26 arm64 (check-6) |
| Tidy | `tidy` | <1s | `hugo: update to 0.167.0`, checkpoint tidy-7 |
| Re-tidy | `restore tidy-7`, `tidy` | <1s | Same files, new trailer, checkpoint tidy-8 |
| Preview | `submit --plan` | ~2s | Check credited to the commit, no other PRs |
| Submit | `submit --yes` | 7.7s | Opened #35000 |

Checked by hand, not by dockhand: upstream's `go` directive is 1.27.0 in both releases, so `go.toolchain_min 1.27.0` still holds. The check log shows lint, fetch, checksum, build, destroot, install and activate with no warnings; hugo declares no tests.

## What worked

- The plan was exactly the edit that followed, and the upstream `go.mod` comparison answered "what changed" without opening the release.
- The restore and re-tidy were painless, and the check stayed credited across the re-tidy because it is keyed to the files, not the commit.
- The submit preview is complete and says everything a submitter should confirm before pushing.

## Friction

1. **The Generated-By trap, again.** The installed binary was a `+dirty` build of `8d8e25cb`. I rebuilt it from HEAD `14320eb7`, which was six commits ahead of origin. Within the hour another session rebased `main` onto a new origin/main, and `14320eb7` became unreachable, so the tidy's trailer already named a commit nobody could find. The merged #34992 carries a `+dirty` version in its body too. dockhand knows its own build info, so tidy and submit could warn when it is `+dirty`. With a GitHub token already present for submit, they could also check whether the commit exists at `herbygillot/dockhand`.
2. **`update --plan` refuses outside a branch.** It gives "hugo is in no open branch, and this checkout is on none" and needs `--new`, though a plan changes nothing. It should plan against master.
3. **`update`'s Next line contradicts the workflow.** "review it with git diff, then commit it" points to a hand commit, when the documented next steps are `check` then `tidy`, and tidy is what commits.
4. **`check` ends without a Next line.** After "Passed for snapshot 1." the next step (`tidy`, then `submit`) appears only in `status`.
5. **`check --plan` "changes nothing" but prints "captured working files as snapshot 1"**, and the real check then reported checking snapshot 1.
6. **Status undersells a check made before tidy.** Following the documented order (check, then tidy) leaves `hugo-9tu8` at "passed for snapshot 1", even after the PR opened. Branches checked after committing show "passed for this commit". `submit --plan` credits check-6 to the commit's files, and `status` should say the same.
7. **`dockhand logs` with no argument** gives cobra's raw "accepts 1 arg(s), received 0". In a branch worktree it could show that branch's latest check.
8. **Repeated words in the output:** `logs` prints "check-6 · passed: passed", and the 14320eb7 submit preview prints "Upstream · upstream: go.mod moves …".
9. **Mixed OS numbering.** Everything says macOS 26 (Tahoe), but the log directory is `tart-25-arm64-1`, which uses the Darwin major.
10. **The PR's Type is left unticked.** This is lower stakes than it looks, because macportsbot labelled #35000 `type: update` from its title. Ticking the box would still make the description agree with that label. Resolved the same day: the person agreed dockhand should tick enhancement for an update it made from scratch, and it now does ([activity](../activity/2026-09-28-enhancement-type.md)).
11. **`status` doesn't show the PR's CI.** Once #35000's three CI jobs had passed, `status` still showed only "passed for snapshot 1" and "#35000". The local check was answering a question the pull request had since answered better. A `PR #35000 · CI 3/3 ✓` cell would tell the user the branch no longer needs them.

## After submitting

MacPorts CI passed on all three runners: macos-14 in 2m52s, macos-15 in 3m19s and macos-26 in 3m39s. That matches dockhand's local 3m14s Tahoe check. macportsbot added the labels `type: update`, `maintainer`, `maintainer: open` and `by: member`, and notified @cardi as co-maintainer. The PR is mergeable and clean.

## Improvements

- **State the Go version check.** For golang ports, say the `go` directive and whether `go.toolchain_min` still holds: "go 1.27.0, unchanged; go.toolchain_min holds". Today silence reads the same as not having looked, and this matters more to a reviewer than module moves.
- **Help find the target port in the log.** check-6's log is 47k lines, and hugo's own phases start at line ~46,400 after its dependencies. `logs --port` could start at the target's first phase, or print a phase summary with line numbers.
- **Show the PR body in `submit --plan`.** The body is what reviewers read and what the checkboxes assert.
- **`outdated` for one named port took 12.5s.** That is long for one livecheck.

## chezmoi, with `bump`

The same afternoon, with a build of `b8915f15` from a clean clone of origin/main, `dockhand bump chezmoi` took chezmoi from 2.72.2 to 2.73.0. It updated, tidied and checked (check-11, Tart macOS 26, passed) in 2m52s, then held the branch for a look and exited 3. After the look, `submit --branch chezmoi-8ndw` opened [macports/macports-ports#35008](https://github.com/macports/macports-ports/pull/35008), with enhancement ticked. This run confirmed four of the fixes above: status credits the check to the commit, bare `logs` shows the latest check, the log directory is `tart-macos26-arm64-1`, and nothing repeats "upstream".

Findings:

1. **A false hold on a Go module.** The hold was "go.mod adds github.com/dustin/go-humanize v1.1.0", but 2.72.2 already required it (`v1.0.1 // indirect`). 2.73.0 only made it direct and moved it one minor version. For a golang port with `go.offline_build no`, Go fetches modules itself, so no module change in go.mod can need a Portfile edit. Holding bump on one makes every Go port that promotes an indirect dependency wait for a person. At most this should say "moves from indirect v1.0.1 to direct v1.1.0" and not hold.
2. **Changes outside the build are noise.** "pyproject.toml moves soupsieve from >2.8.3 to >=2.9" comes from chezmoi's docs tooling (mkdocs), which the port never builds. A Go port's comparison could leave out Python manifests, or keep them apart from build files.
3. **`update --plan` still refuses on an untracked branch.** The fix plans on master when the checkout is on master. When the checkout is on a branch dockhand doesn't track (here the person's `git-devel-2.56.0`), `update chezmoi --plan` still refuses with "No tracked branch: git-devel-2.56.0 is not tracked; dockhand adopt tracks it". A plan changes nothing, so it could plan on master there too. Suggesting `adopt` sends the person the wrong way.
4. **bump's output arrives in bursts when piped.** Its standard output stayed empty for about a minute while it fetched, updated and tidied, then arrived all at once. The check's progress lines streamed normally. This is by design: progress goes to standard error line by line, and standard output carries each step's result when the step finishes, so update's and tidy's results arrive together.
5. **The held branch's message works.** "passed its check and waits for your look, so nothing was submitted: <reason>", then "Once it's fine: dockhand submit --branch chezmoi-8ndw", said exactly what to do. The preview's `!` marker showed which upstream line caused the hold, but a legend would help ("! holds the branch for a look").
6. **A no-op update printed an empty version**, "jq is already at ; nothing to change." This was seen with builds 75d0a05e and 9f1f3ee8, which predate c1d8614f ("a port already current says what it's at"). With b8915f15 it prints "jq is already at 1.8.2; nothing to change."
