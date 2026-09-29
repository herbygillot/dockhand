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

## Cleaning up

With a build of `0ffed143` from a clean clone of origin/main, `status --refresh` found all eight open PRs merged, including #35000 and #35008. `clean` previewed a worktree, local branch and fork branch for each, and `clean --yes` removed all 24. It left `duckdb-cxx14` alone (uncommitted edits, no PR). The preview, with each branch's "merged at <commit>" and "--yes removes these", was clear.

What followed was not:

1. **`status --all` treats every cleaned branch as a problem.** Each of the eight appears under Needs you as "its Git branch is gone" → "dockhand adopt <new name>, if you renamed it", with Work "branch gone" and PR "#35000 merged, not pushed". clean removed those branches on purpose, and the fork branch was pushed and then deleted, not never pushed. The record should say the branch was cleaned, show nothing under Needs you, and give the PR as simply merged.
2. **`status <branch>` can't find a cleaned branch.** `status hugo-9tu8` says "no tracked branch named hugo-9tu8", though clean's help says "The branch's record stays, so status --all still finds it" and `status --all` lists it. Naming a branch should find whatever `--all` can.
3. **clean ignores legacy branches.** The ports checkout still has 22 local `dockhand/bump/<port>-<id>` branches from 2026-09-13 to 2026-09-24, earlier dockhand's naming. They aren't tracked, so nothing reports or cleans them. A one-time notice, or `clean --legacy`, would stop them piling up.

## sshuttle, through paths not yet taken

With a build of `0ffed143`, sshuttle went from 1.3.2 to 2.0.0 ([macports/macports-ports#35011](https://github.com/macports/macports-ports/pull/35011)). It is a `python` PortGroup port fetched from PyPI, with declared tests, and the run deliberately used paths the earlier ones hadn't: `start sshuttle-2`, then `update` inside it, `diff`, `impact`, `path`, `check --plan`, `check -d --on tart:sonoma,tahoe --on github --tests required`, `serve --drain --no-notify`, `wait check-12`, `watch` without a terminal, `submit --draft`, a repeat submit, `submit --ready`, `review --markdown` and `explain`.

check-12 passed on Tart macOS 14, Tart macOS 26 and GitHub (MacPorts' workflow in the fork's Actions) in about 3 minutes, and the summary grid was clear. With `--tests required` the tests really ran: 82 passed on each Tart release. The draft's description was refreshed from Tested on down, listing all three environments. A second submit found "nothing new". Update's and check's Next lines, and `check --plan` recording nothing, all behaved as fixed.

I compared the releases by hand, since dockhand couldn't (finding 1). The only breaking change in 2.0.0 is Python ≥ 3.10, and the port uses 3.13. The sdist's sha256 matched dockhand's. pyproject changed only development-tool bounds, and two test files are new.

Findings:

1. **PyPI ports get no upstream comparison, so they are always held.** "Upstream archives not compared: the current version's archives could not be fetched: portfile: unsupported source edit: only direct HTTP(S) or FTP master sites are supported." The new archive fetched fine for the checksums; only the old one's fetch goes through a path that refuses `pypi:` master sites. Since what couldn't be compared holds (D4), every `bump` or `update --submit` of a python PortGroup port stops for a look, whatever changed.
2. **`submit --ready` fails on macports-ports.** GitHub refuses it: "the `macports` organization has enabled OAuth App access restrictions". The description update in the same run succeeded, so the PR ended up refreshed but still a draft. The exit code was 1, correctly. `gh pr ready 35011`, through the GitHub CLI's own OAuth app, worked. The failure should say what to do, such as "mark it ready on GitHub, or `gh pr ready 35011`". If dockhand's OAuth app can be approved for the org, the docs should say how.
3. **`--ready` can't be previewed.** `submit --ready --plan` is refused ("--plan … goes without … --ready"), so the one step that changes a PR's state is the one without a preview. The plain preview then says "mark it ready for review on GitHub when it is", without naming `dockhand submit --ready`.
4. **The submit refusal ignores a queued check.** With check-12 queued, `submit --plan` said "no check has finished … run dockhand check first", and the Checks line said "none has finished". It should name check-12 and `dockhand wait check-12`.
5. **`wait` with no argument** fails with cobra's raw "accepts 1 arg(s), received 0", as `logs` did before its fix. In a branch's worktree it could follow that branch's queued or running check.
6. **serve's banner and the scheduler disagree.** The banner said "builds on github (2 at a time), tart (1 at a time)", but the Sonoma and Tahoe VMs started, built and passed at the same time.
7. **The GitHub check leaves its branch on the fork until clean.** `dockhand-check/e15ced625514` stayed on herbygillot/macports-ports after check-12 finished. Once #35011 merged, `clean` removed it and check-13's `dockhand-check/7e7c6cd17724` along with the branch, so they don't pile up. They do linger for the life of the PR.
8. **The GitHub line in the description says "Developer tools not recorded".** The runner's macOS and Xcode could be read from the workflow log, as Tart's are from the guest.
9. **The logs are still very long.** Each Tart log is about 72k lines, and sshuttle's own phases start around line 70,900 (see Improvements).

Worked well: `watch` without a terminal prints status once, then streams events. `explain` lists its rules, quotes MacPorts' guide, and refuses an unknown code helpfully. `review --markdown` gives a short, correct review. `path`, `diff` and `impact` are quick and clear.

### What a comparison would have caught, and what the build did

Two things about sshuttle 2.0.0 are exactly what dockhand's upstream comparison exists to report, and it reported neither, because it couldn't compare PyPI archives (finding 1):

- `requires-python` moved from `>=3.9` to `>=3.10`, and the classifiers dropped 3.9 (they list 3.10 to 3.12; upstream's tox and CI test only those). The port pins `python.default_version 313`, so it is unaffected. For a Python port, though, a raised floor, or a ceiling below the port's pinned version, is the Python equivalent of Go's `go` directive and deserves a line.
- The python PortGroup's default is now 314 (`python_get_default_version`), while sshuttle pins 313. Neither `outdated` nor `update` mentions a pinned version that lags the PortGroup's default. That's a candidate for a quiet note rather than a hold.

By hand, outside dockhand, the 2.0.0 build was exercised in two throwaway clones of `dockhand-xcode-tahoe`: a client with sshuttle installed from the branch's Portfile, and an SSH server running macOS's own Python 3.9.6. Tart's shared network doesn't let the guests reach each other, so the client reached the server through an SSH port forward over the host. Through that, a single-address tunnel (1.1.1.1/32), a full tunnel (0/0) and a full tunnel with `--dns` all carried traffic. The pf anchor used `pass out quick` (2.0.0's fix), the remote's own address was auto-excluded (2.0.0's feature), and every rule was flushed with pf disabled when sshuttle stopped. A dockhand command that kept a check's passing guest, or started one from an installed archive, would make this kind of by-hand test a single step.

### Moving sshuttle to Python 3.14, and re-submitting

The person asked for sshuttle to build with Python 3.14, the PortGroup's default, if it worked. I edited `python.default_version` to 314 by hand, then ran check-13 on the working files (`--on tart:sonoma,tahoe --on github --tests required`, in the foreground without serve). It passed everywhere, and all 82 tests passed under py314 on both Tart releases. `logs` showed the GitHub run's per-runner logs (macos-14, 15, 26), which was useful. I repeated the hands-on tunnel test in two fresh guests, with python3.14 on the client and stock 3.9.6 on the server, and got the same results as with 3.13, with no warnings. Then `tidy --plan --out`, an edited message, `tidy --apply`, and `submit` replaced #35011's history.

What went right: `status` read "1 commit + edits" and "passed for older work" before the check. Tidy dropped Generated-By from a commit mixing dockhand's edit with a person's, and said "has changes dockhand's commands did not make". Apply warned "#35011 still shows 1 commit until you submit; submit will replace its history, if no one else has pushed". The push was conditional ("the fork's branch was where submit saw it"). The check stayed credited after the tidy, and the enhancement tick survived even though the commit is no longer dockhand's alone.

Findings:

1. **A saved tidy plan is JSON.** "Edit its messages there if you like" means editing `\n`-escaped strings inside JSON. A plan that shows each message as plain text, like `git rebase -i`'s reword or a message file per commit, would suit people.
2. **`tidy --apply` doesn't show the message it applies.** It reprints the proposal, but not the edited message, so the body I added was visible only in `git log` afterwards. It also says "subject from your commit e15ced6", when that subject was dockhand's own, from the update.
3. **A re-submit never refreshes the Description section.** It refreshes "from Tested on down", so a commit body added after the PR opened ("Build with Python 3.14, the python PortGroup's default.") never reaches the PR, and #35011's Description stays empty. As with Type(s), the Description could be refreshed while it is still exactly as dockhand wrote it.

### Cleaning up after sshuttle

With a build of `f85f200b`, `clean --yes` removed sshuttle-2's worktree, local branch, fork branch, and both of its checks' `dockhand-check/` branches. Afterwards, the findings from Cleaning up are fixed. `status --all` lists every cleaned branch as "cleaned" with its PR "merged", and nothing appears under Needs you. `status sshuttle-2` says "cleaned after its merge". Only the 22 legacy `dockhand/bump/*` branches (finding 3 there) remain for a person to delete.

## certigo, with a hands-on binary test

With a build of `d7682668` (its first run migrated the main database, silently), certigo went from 1.18.0 to 1.18.1 ([macports/macports-ports#35013](https://github.com/macports/macports-ports/pull/35013)). Paths new to this exercise:

- `update --new --json`;
- a no-op `checksums`;
- a duplicate `check -d`, which was refused with "check-14 is already queued for these files; dockhand wait check-14 follows it";
- `cancel check-14`, and `check -d --replace` over a queued check;
- Tart macOS 13 (Ventura);
- `status --attention` (exit 3) and `status --json`;
- a no-op `rebase`;
- `check --head`;
- `submit --tested-binaries`, then a re-submit with `--type enhancement --type bugfix`, which refreshed the open PR's Type(s).

check-16 (working files) and check-17 (the committed head, adding GitHub) passed. The plan-format and apply-output fixes held: the saved plan is TOML with the message as a plain multi-line string, and `tidy --apply` printed the body it applied. The first submit carried the commit body into the Description.

**The binary test found what every check passed.** In a throwaway `dockhand-xcode-tahoe` guest, certigo 1.18.1 connected and verified www.macports.org (TLS 1.3) and refused a wrong expected name (exit 1). It also spoke STARTTLS to smtp.gmail.com:587, dumped a PEM chain and a PKCS#12 bundle made with the system openssl, refused a wrong PKCS#12 password (exit 2), printed JSON, showed zlint's warnings, and failed a self-signed cert's verification. But `certigo --version` printed `(devel)`. 1.18.1 replaced its hardcoded version string (1.18.0 still said "1.17.1") with Go's build info, which a tarball build doesn't have; upstream documents `-ldflags "-X github.com/square/certigo/cli.version=…"` for such builds. The Portfile now passes it, and a rebuild in the same guest printed `1.18.1`. Lint, fetch, build, destroot, install and the declared tests all passed without it. Only running the binary shows it.

Findings:

1. **Running each installed binary's `--version` would catch this class of regression.** After install, in the guest, each executable the port installs under `${prefix}/bin` could be run with `--version`, flagging output that lacks the port's version, as advice rather than failure. Version strings are what reviewers and `port installed` users compare.
2. **A person's edit on top of dockhand's uncommitted update costs the subject.** With the update not yet committed and a hand edit to the same Portfile, tidy proposed "(needs a subject)", with a ✗ finding. The version change and its recorded subject "certigo: update to 1.18.1" are still dockhand's. The subject could be kept, with the note that other changes are included. (In sshuttle's case the update was already committed, so the subject came from that commit.)
3. **`update --json` keeps the "upstream: " prefix.** Its upstream messages still begin with it, though the text output dropped it.
4. **`cancel` with no argument** gives cobra's raw "accepts 1 arg(s), received 0", like `wait`. Inside a branch it could cancel that branch's queued or running check.
5. **"Stopped check-15; what it finished is kept."** was said of a check that was only queued and had finished nothing.
6. **`status --json` gives serve as a sentence.** Its `serve` field is the preformatted "serve: running (pid 34857) · queue: 1 run", not fields a script can read.
7. **The PortIndex rebuild is only visible in serve's log.** serve printed "Building the PortIndex; this may take several minutes" in the middle of check-16, which took 5 minutes. A one-time cost after the migration seems likely, but the person following with `wait` never saw that line.

Not exercised yet: `retry`, `adopt`, `archive`, `edit`, `revbump`, `create`, `start --here`, `check --also/--only/--baseline/--fresh`, and `submit --passing`.
