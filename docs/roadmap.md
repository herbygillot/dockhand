# Dockhand roadmap

This is the source of truth for what gets built next and in what order. [Design v3](design-v3.md) defines behavior, [architecture](architecture.md) maps it onto the code, and [usage](usage.md) is the guide. What was decided, and what was declined, is in [decisions](decisions.md); what each release carries is in its notes in [releases](releases/); what was done, and why, is in Git's history, and the documents this roadmap once linked are at tag `docs-archive-2026-10-25`. A done entry cites its commits and leaves once its release's candidate is cut, the release's notes carrying it from there and the commit that removes it saying so. Every list meant for implementation, a review's, field testing's, or a session's notes, is folded in with a coalescing run across the roadmap before it's built (the person, 2026-10-19 18:45Z and 18:47Z).

## Where dockhand stands

The whole loop is built and used for real work, MacPorts pull requests and dockhand's own port among them. On main:

- **Branches:** `setup`, `start`, `adopt` (`--pr` for someone's pull request), `path`, `archive`, `clean`, and `undo`.
- **Authoring:** `update`, `create`, `checksums`, `revbump`, and `edit`; `ship` updates, checks, and submits, and `bump` does it asking nothing.
- **Understanding:** `status`, `outdated`, `diff`, `impact`, `explain`, `config`, and `open`.
- **Checks:** `check` in Tart (macOS 12 through 27, each with an Xcode image), on the fork's GitHub Actions, through a command of your own, or in a MacPorts prefix on this Mac (`local`); `logs`, `retry`, `shell`, `artifacts`, `wait`, and `cancel`.
- **Review and submit:** `tidy`, `rebase`, `submit`, `review`, and `decide`; `serve` runs queued checks and the daily look at the selected ports.

v0.5.0 is the latest release (2026-10-25, 6246e4b9, its port updated in macports/macports-ports#35968). v0.6.0's last candidate is in acceptance, and v0.7.0 is on main behind it, with Codex's review of main to fix before its rc1.

## Next

### v0.6.0: acceptance, rc6 and then rc7

rc6, 0dbf87e2 (tagged 2026-10-27 05:42Z), carries Then's items 1 to 5, the Portfile corpus, the `local` provider, Link Dupont's syft fix, Codex's language review, and the fixes found on the way; [its notes](releases/v0.6.0.md) say what's new and its six known issues, none harm. The M1 runs its acceptance as v0.5.0's ran:

- Done: A7 from rc5 under serve (schema 42); the quick stage, 49 passed and none failed; the full stage, 79 passed and none failed, no harm.
- Running: B7 and C1 overnight, to about 19:30Z on 2026-10-28; then G3 on a non-root MacPorts, whose install is built.
- Then the person's sitting, started by his "I'm back" in the Prime-time thread: A2, A8, B4, D-C3, D-C4, D-I5, F5, and F7, and G1, G2, and G4 with sudo.
- Then the write-up, naming as not covered D-R1, a check that can't fit on disk, and F6, worktrees on an external or case-sensitive volume, and F7, a prefix other than /opt/local, unless G3 covers it; and the tag and its GitHub Release at the person's typed word in the implementation thread.

**rc7, at the person's "Go ahead and cut rc7" (2026-10-28 12:04Z, in the architecture thread), for two harms Codex's review of main (8e314b96) found in rc6** (the architecture review's assessment of 2026-10-28, which ran each probe on main; both hold on rc6's code). Each is a fix on main first, for v0.7.0 below:

- **The `local` provider owns its prefix for a whole check** (finding 1; done 2026-10-28 on main, 56b3ce40 and 234155a7; on release/v0.6, 993e093a and 660ac1cb). `Local.Execute` (buildenv/local/local.go) restores what was left, snapshots, builds, and puts the prefix back with nothing keyed on the prefix, and the engine's slots count checks per provider, not per installation, so two checks on one prefix, serve's and a person's, or two sessions on one Mac, can snapshot each other's half-built state and put it back. As done, a file lock keyed by the prefix's canonical path is held from the restore through the put-back, in /var/tmp/dockhand-locks-<uid>, so it's the person's across databases and homes, not the machine's; its holder is a process by PID and start time, in procid, which coord re-exports, so a reused PID no longer reads as a live check; and a second check waits within its context, "waiting for /opt/local, held by check-12 (pid 4711)". Then, at implementation's note that a check by another user on the same prefix didn't wait, as dhtest's and the person's could on the M1's /opt/local, the lock moved to /var/tmp/dockhand-locks, sticky and open to all as /tmp is, taken on a file all can open to read, since flock locks a read-only open, neither path following a link, and each user's holder writing its record beside it (234155a7). status naming the holder, as the wait's line does, is v0.7.0's, below.
- **A license's party rename is only a name** (finding 2; done 2026-10-28, 6ae97563 on main, fccc85d5 on release/v0.6). `partyOnly` (sourcecompare/compare.go) takes any word pair replaced in two or more short hunks as a rename, and `sameLicense` counts two licenses it doesn't know as the same, so "must" to "may" twice, or "version 2" to "version 3" in a header and a footer, is said as a party rename and doesn't hold (`license-party`, assess). The exemption needs both replaced words name-like, capitalized and with a letter, and neither one of a license's own words, which take in RFC 2119's MUST and MAY and the licenses' names; anything else is a changed license, which holds. `sameLicense` stays as it is: licensecheck classifies neither of vault's BUSL files, so "unknown never equal" would hold vault's rename again (implementation's reading). As done, a replaced word named before is no new party either, and must to may, version 2 to 3, MUST to MAY, GPL to AGPL, Apache to MIT, and Acme to an already named Zenith all hold, while vault's HashiCorp to IBM stays a rename.

rc7 is cut on release/v0.6, a branch from rc6's commit, its commit 660ac1cb with the three fixes, since main carries v0.7.0, its tag pushed at the person's word typed in the implementation thread as every tag is, before his sitting if it's ready, so the sitting's `local` rows run on rc7 and he sits once; the M1 runs A7 from rc6 to rc7, the `local` rows, G1 to G4, the license-hold row, and a new row of two checks on one prefix at once, one as each of two users, and the rest of rc6's acceptance carries over. Its notes, which name rc7's commit (58504471 and 06cb12fd), take two known issues from the same review: comparing two license files can use memory quadratic in their words (finding 5; 51 MB for a 25 KB pair), and a self-hosted GitLab whose instance URL has a path is refused for source archives (finding 8).

**A known issue, at the person's "known issue" (2026-10-28 13:19Z, in the architecture thread), not rc8: a guest a crashed dockhand leaves building** (the architecture thread's finding, sent here by the coordinator at 13:19Z). The guest program runs under sudo in its own process group and outlives a dockhand that crashes, where the lock dies with dockhand; `left()` skips a check's directory whose owner or guest still runs (`building`, buildenv/local/local.go), so `restoreLeft` never sees the orphan, and the next check, holding the lock, snapshots and builds beside a build still going. The prefix is left wrong, a harm by the bar's letter, but it needs a dockhand crash and then a second check on that prefix within the orphan's build, every version with `local` has had it, and status already says the prefix is building. Its fix is on main for v0.7.0 (272e968b), and v0.6.0's notes say: "After a dockhand crash, wait for the check's port-tclsh to finish (status shows the prefix as building) before another check on that prefix; a check started sooner builds beside it."

Its notes take one more known issue from B7, not harm: serve with nothing to run spent about 15% of a core over 16 hours on dhtest's database of 838 branches, fixed in v0.7.0 (below). A harm found makes rc7 with its fix and reruns the rows the fix touches; anything else is a known issue, or rides v0.7.0. After the tag, dockhand's port is updated through dockhand, and moved to a new `macports` category directory, at the person's word (2026-10-27 03:22Z), if the tree takes one; if it doesn't, or dockhand can't make the move, he's told why.

### v0.7.0: on main, one release

Then's items 6 to 15, and everything placed beside them, are done on main as one release (the person's "One release", 2026-10-26 06:22Z), but for the items placed before rc1 below. Its rc1 is cut after v0.6.0's tag, at the person's word, and as it's cut the README's known issues become v0.7.0's, the list [its notes](releases/v0.7.0.md) carry. It migrates the database through schemas 43 to 49.

Its acceptance is v0.6.0's, with:

- the full stage building one older Tart release too, so `minmax` runs as two releases, with two VMs at once (S2 and B7) and the wait for them (the docs dig's I29);
- a selection's ports compared with `port echo`'s on the M1's tree;
- dhtest's `status --all` measured against rc2's 55 s and 3,962 git processes, its output compared (item 6's measure);
- an update moving `github.tarball_from tarball` to `archive`, checked in Tart;
- C8 applying a fresh saved plan whose branch takes an untracked patch that patchfiles names, and a lint fix, where rc6's C8 tries only a stale plan's refusal;
- G5, two outdated subports of one Portfile updated in one branch and tidied into a commit each, titled for its subport (ACCEPT_SUBPORTS);
- B7's serve measured idle on dhtest's database against rc6's 148 CPU-minutes in 16 hours, its log holding no line that isn't serve's (below);
- two `local` checks on one prefix at once, the second waiting for the first's put-back (finding 1, above);
- a `local` check whose dockhand is killed mid-build, and another on its prefix, which waits for the first's guest program to end before it snapshots (272e968b).

Placed before rc1, in the wait for v0.6.0's tag, so v0.7.0 stays one release (rc6's B7 on the M1, which passed, sent here by the coordinator on 2026-10-28 at 11:25Z; not harm):

- **An idle serve costs little** (about half a batch, inferred). On rc6, serve spent 148 CPU-minutes in 16 hours with nothing to run, about 15% of a core, on dhtest's database of 838 branches. While idle it polls every 2 seconds, reading the store for its lease and queue, and every 5 minutes refreshes the pull requests and gathers the status of every open branch for its submit pass (engine/servesubmit.go `PassingBranches`, through `Status`), the per-branch reads that make v0.6.0's `status --all` slow; on main, item 6 reads each kind of git fact once for every branch (ff7d16cc), which may be most of it. Its first step is done: the submit pass reads git only for the branches a check finished for, which the store says (4f75365e). The rest waits on an hour's profile of an idle serve on main over dhtest's database, on the M1 once C1 and G3 are done, and fixes where the time goes; the suspects left are the 2-second loop's store transactions, `session.Fenced`'s write every tick and `Candidates`, and the pull request refresh. Done when an idle serve on dhtest's database spends a small fraction of rc6's rate, measured in v0.7.0's B7. Taken when touched's serve finding 3 rides it where the change is in serve's loop.

From Codex's review of main at 8e314b96 (2026-10-28, its ten findings all holding on main, each probe failing as it says; the architecture review's assessment, sent here by the coordinator at 12:00Z), before rc1, in this order, about 2¼ batches together for what's left (inferred). Findings 1 and 2, above, came first, on main and then ported to rc7's branch; 4 and 10, the two that let a submission pass that should hold, and 3, where a requested test policy was lost, are done (b2267cdc, 9938c362, and 45e3efae, in What it carries); then 6, where an acceptance is lost, then the two that say the wrong thing, then rc6's other two. Each of Codex's probes becomes a test at its owner, named as the package names its tests: finding 6's in engine/submit_test.go, 5's in sourcecompare/compare_test.go, 7's in engine/installed_test.go, 8's in upstream/source_archive_test.go, and 9's two in command (status's and `review --check`'s projections), as 1's, 2's, 3's, 4's, and 10's already are (56b3ce40, 6ae97563, 45e3efae, b2267cdc, and 9938c362); their source is kept in the project's files until each is a test:

- **Status loads every decision a standing reads** (finding 6; about half a batch). Status loads decisions only when a target failed (engine/status.go), where `Publishable` reads a lint acceptance on a passed new port and submit loads it anyway, so status, serve, and `ship --passing` can say a branch isn't ready that submit takes. One acquisition loads the decisions of every finding kind whose standing reads one, failure, environment, undeclared link, hand edit, and lint, for status, submit, serve, and ship alike, and an agreement test across them guards it.
- **What moved is read against the check before** (finding 7; about a quarter batch). `InstallChanges` (engine/installed.go) compares with the newest reusable run but this one, so a later check becomes an earlier one's baseline: "libjq.2.dylib is libjq.1.dylib now", said of check 1 after check 2. Its baseline is the newest reusable run started before it, or the one its plan names, a query of its own, recorded on the check so status says which.
- **JSON carries what the text says** (finding 9; about three quarters of a batch). status's JSON is `BranchView` alone, where its text says decisions, instructions, lint, links, and what installed or configure changed; and `review --check --json` loses the review, `check`'s emit replacing it (term/json.go). Each verb's renderers read one gathered payload, its JSON a projection of it, check's carrying the diagnostics its text gathers too, and `review --check` composes its two results through the gatherer and `LinkSteps`; a test has the JSON carry each section the text renders.
- **One bounded way to match sequences** (finding 5; about half a batch). The license's `wordHunks` (sourcecompare/compare.go) builds a whole matrix, 51 MB for a 25 KB pair, beside CMake's bounded `changedLines` and two more in portfile (lintfix.go, contexts.go). One bounded primitive, `changedLines`' trimming, int32, and budget, serves all four, and past its budget the answer is "changed", which holds; Taken when touched's CMake wording file rides it.
- **A GitLab instance keeps its path** (finding 8; about a quarter batch). `gitRepository` (upstream/source_archive.go) compares `https://` and the host with the configured instance, so `gitlab.instance https://gitlab.example.com/root` is refused. The instance is compared as a URL, scheme, host, and path prefix, and the project's path read under it.
- **status names a held prefix's holder** (from implementation's note on 56b3ce40; about a tenth of a batch). The wait's line names it, "held by check-12 (pid 4711)", and status doesn't. Done when status, during a `local` check, names the check holding its prefix.
- **check's refusal beside a check that doesn't cover it offers `--replace`** (from finding 3's fix, 45e3efae; about a tenth of a batch). Beside a running check of the same files that doesn't cover the one asked, check, ship, and `review --check` all say "Run dockhand cancel check-1 and run this again, or wait for it to finish and run this again" (engine/queue.go), where check's other refusals offer `--replace`, which stops it, keeping what it finished, and checks this one (command/check `besideWords`). check words this refusal with `--replace` too; ship and `review --check`, which have no `--replace`, keep cancel or wait. Done when check's refusal names `--replace`.

What it carries, each said in full in its notes:

- **Updates.**
  - An update to a lower version raises the epoch (bcdcb247).
  - An update's commit names its upstream release, at best effort (c2c96d35).
  - A stealth update says whether the archive's contents changed (0cbe29e8).
  - A Go port's `-X` target is still declared (68641921).
  - A C++ standard upstream raised is said against `compiler.cxx_standard` (8cab3958).
  - Literal Homebrew library paths an update adds are said (32085c72).
  - A repository move says when the old owner's name now belongs to another account (a2e81dc1).
  - The patch refresh, item 8 (fd9de5a2, 4ba655e2, and 2cdedbf3).
  - A library's install names, and its dependents, item 9 (b271486c, a146a9fe, c750b273, and 7d5bcf4d).
  - The configure summary's drift, item 10 (881cc8b9).
  - create's deferred work, item 14: Cargo targets found once, a workspace's binaries, `create --plan`, and a Go module below its repository's root (35b36ad5, 1fb335ca, and 622c5bd1).
- **Lint and notices.**
  - dockhand fixes three lint findings on the lines a change touches (652d5e05), and check makes them before it captures (6633d1bd).
  - The survey's other lint-shaped findings are notices (9a1f4ff9), and the survey rules are counterfactual notices on the ledger, item 11 (24651a28 and cb72a01c).
  - A fix beyond the change is asked for, with `tidy --restyle` (c4229f64).
- **Checks.**
  - Tart's limit is the Mac's VMs, not a count of checks (391e76e1).
  - check and `review --check` follow a check already running for the same files only where it covers what they ask, by the engine's rule, and say what it doesn't cover (614951e1 and 45e3efae).
  - A failed build's summary says the tool's error, and a queued check nothing is running says so (cf5e18c2).
  - A check says how long each part took, and setup says whether go2port and cargo2port are there (42fcbc3f and 0d3d2ffc).
  - Next names the check a passing host check didn't build (03136805), and its variant check names its port and follows minmax (cab3ce9b).
  - After an amend, what still needs a pass names its platforms (b719703a).
  - An undeclared link holds, read from the check's merged evidence, and regression tests keep it (96af4d95, a2e81dc1, and b2267cdc).
  - After a dockhand crash, a `local` check waits for the guest it left building before it snapshots (272e968b).
  - `dockhand artifacts` prints a check's built archives (bd7043f1).
  - Under serve, a command's progress reports are serve's own "serve:" lines, and its fetches of master say nothing (7bc1e35c).
- **Selecting, holding, and submitting.**
  - Port selectors, and serve's selection (6daa852a).
  - The trust rule's attended half, with `review --check`, item 12 (1fce8d50, ee4a792c, and 26c06a4a).
  - The person's voice, item 13 (ce68f1a9).
  - After submit, a follow-up branch and CI's error line, item 7 (6d41829c and 78fb9727).
  - The pull request's body fits GitHub's limit and says what `--except` left out (1f90bc79), and a rebuild whose every dependent was left out is listed too (87102335).
  - A saved tidy plan whose branch adds an untracked patch is no longer refused as stale (a8a40367).
  - Several ports of one Portfile go in one branch, `update` taking several ports and a selection grouping them by Portfile (8e314b96 and 93d2e8cc), and tidy makes a commit for each update, titled for its subport (644faca7); where it makes one commit for several, its subject names each, and the person's own message for dockhand's edits is kept (4fe17cf9).
  - serve's check for a submitted pull request reads a subport on master (8e314b96).
  - A removed flag, like a removed setting, says what replaced it (921c1bf9).
  - Every commit rule that holds can be allowed by `holds.allow`, `version-order` and `replaced-by-unchanged` among them, its catalogue entry carrying its severity; and a tidy commit's hand edit is a field, not a sentence read back (9938c362).
- **Status and cleaning.**
  - Status gathered once, item 6 (ff7d16cc), its measure an acceptance row above.
  - clean removes a merged branch whose changes are all on master, and a listing can end (650ec969); it offers the fork branches closed and canceled work left (d887556f).
- **Inside.**
  - The assessment as a package, item 15 (ab7dce32, bd92981b, and 6441e960).
  - A modelled context's SDK, tools, and compiler list are its own on base master, from the weekly job's findings (33251823 and 0c1aa135).
  - Two wordings (b719703a), a test's wait bounded by the suite's settle (eb0aaebd), and the corpus comparing a plan's Portfile diff alone (7152c9ad).
  - Its notes, drafted (622c5bd1) and brought up to date (1abbe967 and 1324df11).

Nothing is placed after v0.7.0 yet; Later is where its items come from.

### Alongside, on the Mac

These need MacPorts Base, whole-tree surveys, or VMs, so they run as the Mac allows, independent of the order above.

- **The oracle's remaining phases** ([scope](oracle.md)):
  - phase 3, the workspace rule: reads inside the tree materialize on demand, and reads outside it are refused;
  - phase 6, the bootstrap under the environment contract;
  - `java_home` answered from the facts table;
  - 4b, `with-deps`, only when a survey shows a registry answer reaching what an update edits;
  - the comparison with a fresh Tart guest, possible since the v3 Tart provider;
  - decision D2 ([decisions](decisions.md)).
- **Host-independence foundations:**
  - the evaluator's clean launch environment (16);
  - the environment contract, and a disposable `portdbpath`;
  - normalized records;
  - `base212`, and a master preview identified by commit (39).

## Decisions for the person

Open, raised by the architecture review on 2026-10-28:

- Whether docs/reviews/ stays: docs phase 2 meant to remove it, and it still holds the review of 8c007979, which Taken when touched links; the review of 8e314b96 stays uncommitted at the person's word.

Decided questions, with the rule each settled, are in [decisions](decisions.md).

## Later

From the gnome-voice session's notes (2026-10-20, 06987cab):

- **What a check will install, said before it starts.** Its check of an unchanged port ran over 10 minutes in the foreground, 4 ports built from source. The plan would say, for each target, how many dependencies it installs, how many from dockhand's kept archives, and which MacPorts has no archive of, by a HEAD of packages.macports.org, a hint only (Decided: saying which build from source), so a person picks `-d` or not. `check -d` without serve takes most of the need away, so it waits. dockhand already keeps every archive a guest installed a dependency from, MacPorts' own included.

From the exiv2 session's notes (passed on 2026-10-21, 402b6ba4):

- **A check of one branch with another's ports laid over it.** To build exiv2 0.28.9 with its patched consumers, kept on two branches for two pull requests, the session merged them by hand into a throwaway branch, exiv2-0.28-verify, and checked that. A first-class form would check a branch's files with another branch's changed ports over them, as evidence for neither, since each pull request is judged against master alone. It waits for a second such update: the throwaway branch is one `git merge`, and `--also`, which builds unchanged dependents against a branch, is the session's next to try, on stage 2's revbumps.

From part 5's shared-flag contract (2026-10-22):

- **`rebase --plan`.** It needs a plan of its own, the commits a rebase would replay and onto what, where every other `--plan` shows a path that already exists; `create --plan` came with v0.7.0's item 14. rebase's checkpoint undoes a rebase today, so nothing is lost meanwhile.

Parked, at the person's word, until someone on macOS 10.13 to 11 asks:

- **Release binaries for macOS 10.13 to 11** (the person's "OK, let's give it a go then.", 2026-10-20 00:46Z, then "Park as draft", 01:11Z, once the one who asked, barracuda156, turned out to be on PowerPC 10.6, which Go can't target). herbygillot/dockhand#6 is a draft, its branch kept, and nothing watches it. MacPorts' Go stops at 1.24 there, and a build with Go 1.25 or later dies at load on `SecTrustCopyCertificateChain`, which only macOS 12 has; on 12, `port install dockhand` already works. The pull request has the x509 patch for `go build -overlay`, `make build-legacy`, the imports check against 10.13's exports, a release workflow with a run by hand for a named tag (v0.4.0's commit has none), and the legacy build said in `--version` without changing `Generated-By`. Taken up again, the imports check runs in every CI run, its binaries are called experimental until someone runs them on 10.13 to 11, and Maintenance gains the rule that each new Go series either applies the patch and passes the check or ends the assets at the last release that passed, the patch never going past crypto/x509's certificate loop.

From field testing at e498a1a8 (2026-10-07):

- **A second release a check missed** (tart-softnet 0.24.0, #35323): it failed MacPorts CI on macos-15 with vmnet symbols macOS 26 alone has, `_vmnet_network_configuration_create` among them, where the check built on Tahoe alone, as `check.on` has it by default. The second such case after tart's (batch 10); the decision that `check.on` defaults to this Mac's release, with its notice, is revisited if the person wants, as `check.on = ci` for ports that link system frameworks, or a `·` naming the APIs a build links that are newer than CI's oldest release.
- **tidy's commit order with `--revbump-dependents`** still puts a rebuild's commit before the update's by default, which batch 84 had put after (finding 3, taisei and libunibreak); `--group "2 1"` and `--plan --out`/`--apply` worked around it. To reproduce first: batch 84's order holds for directories dockhand edited in one update, and the case is likely the revbumps' own record coming before the update's.

From Codex's gcc15 group update (2026-10-07):

- **A binary older than the checkout it's run in says so** (Codex's gcc15 note 7, 2026-10-07: a build of 464d583c, 86 commits behind main, met fixes main already had). dockhand knows the commit it was built from; where the ports tree's own checkout isn't dockhand's, the comparison needs dockhand's source checkout, which only a development build has, so it's a note for development builds alone, "this dockhand is N commits behind <checkout>", or a `version` line. A release names its tag, which is enough.

From field testing's batch 15 (2026-10-06, at 6f9d714d):

- **A patch that only edits Cargo.lock, on a version update,** is said as a lockfile refresh the new release may make unneeded, not "edits dependency manifests; regenerate manually" (meli's patch-Cargo.lock.diff); a check could try the build without it.

From field testing's batch 14 (2026-10-05, at 82551b6d):

- **A feature newly pulling a crate that downloads at build.** qsv 24.0.0's distrib_features enabled viz_static, which brings webdriver-downloader, whose build.rs downloads chromedriver. Saying it needs a signal more reliable than a crate's name, such as reading a new crate's build.rs for a network client, which the comparison doesn't read yet; a hold, as field testing asked, was the person's call, as D9's is: a notice, not a hold, until the guest can observe a fetch during the build, when it's a layer 1 finding (the coalesced roadmap's decision 5, 2026-10-13).

From the full stages and field testing before them:

- **Fixtures for D-N2 and D-N5** (the rc6 full stage, 2026-10-07): a distfile gone upstream with a newer release, and a tag moved after a check, which neither the run nor MacPorts' tree can be relied on to have. dockhand finds releases on GitHub's API, so a local repository doesn't stand in; a test repository of the test account's, whose releases the harness deletes and re-tags, would, and is the person's to allow. Both rows aren't run until they're named one (ACCEPT_DN2_PORT, ACCEPT_DN5_BRANCH).
- **Choosing a GitHub port's source for it** (the person's wish, 2026-10-03, with mindforger #35195). Since batch 85 an update says a port still fetches GitHub's tarball, with the order MacPorts prefers, releases then archive. Choosing for it means knowing whether a release asset is a source tarball, and for a port fetched with Git for its submodules, whether the asset's top directory holds them, as mindforger_2.5.0_tarball.tgz holds deps/cmark-gfm, where a plain archive wouldn't; then the edit, distname, extract.suffix, worksrcdir, dropping fetch.type git and the submodule step. And `checksums` adding a checksums block for a port moving from a Git fetch to a distfile, where it now refuses for want of one ("no checksum declaration for mindforger_2.5.0_tarball.tgz").
- **A Python requirement weighed by what needs it** (field testing's py-mlx-vlm follow-up, 2026-10-04). Its review counted 12 missing requirements alike; only Pillow and requests broke `import mlx_vlm`, tqdm came through transformers, and the rest were for `mlx_vlm.server` or imported lazily. A requirement a dependency's own dependencies provide would count as met, which needs their evaluations in turn; and one reached only through an optional or lazy import would be said apart from one a module-level import from the package's `__init__` needs, which is reading Python's imports. Both wait behind the core.
- **A local `port lint` before a check that looks like a hang** (the Vx port's field testing, 2026-10-03): it took long enough while the port index was built for a new port that dockhand should say what it's doing, or reuse the index the check's evaluation already built.
- **A project that says it moved** (field testing's eighth report, 2026-10-02). pomo's GitHub repository's last commit, from 2022, is a notice that it moved to Codeberg, where 0.8.2 is; outdated counts it current at 0.8.1. Reading that from a repository, its archived flag or a long-quiet push with a migration notice, is a new check, so it waits behind the core. The shape where a repository's tags stop below the port's version, mergestat's, is held as ahead since v0.6.0; pomo's, whose own tag is the newest, stays here.
- **A license group that makes a port non-distributable** (the Vx language port session, 2026-10-19). `license Apache-2 {LLVM Exception}` reads, as the Guide has it, as Apache-2 and a choice of LLVM or Exception, which submit's comparison said rightly; but MacPorts' distributable checker wants each brace group to hold a license it knows, and neither name is one, so the port gets no binary archives (lang/wasi-sdk has the same line on master). A `!` finding would name a license the checker doesn't know, with the line to write (`Apache-2` for Cargo's `Apache-2.0 WITH LLVM-exception`, as devel/wasmtime has). The checker's good_licenses and license_exceptions are macports-infrastructure's jobs/distributable_lib.tcl, not a documented interface, so the person decides first whether dockhand reads that list, keeps its own copy pinned by a test, or counts the names the tree uses.
- **Running what a port installs** (the certigo run's finding 1; out of consideration at the person's ruling on field testing's scope items, 2026-10-23 20:10Z). Each executable a port installs would be run with `--version` in the guest after install, with advice, never a failure, where the output lacks the port's version. certigo 1.18.1 printed `(devel)`, since its version moved to Go's build info, which a tarball build lacks. Every check passed, and only the binary showed it.
  - Running built programs unattended has side effects to design for first: some start servers or wait on input, so each needs a timeout and no input, and a port with many executables would flood the result. The executables would come from MacPorts' own `port contents`.
  - Not before dockhand's core capabilities are solid, as the person decided on 2026-09-28.
- **Evidence across repository registrations.** One database serves several checkouts. Reuse keys evidence by tree already (reuse/choose.go), so what's left is whether identical inputs from another checkout are trusted.

From the exiv2 and bazel sessions' notes (2026-10-24, 95e78117):

- **A check's guest, running or failed, to work in** (the exiv2 session's rc4 notes and the bazel session's, which found no way into a running check's VM). `dockhand shell` enters a VM a check kept with `check --keep` (42f2bb1a); what's left is a command in a running check's guest, and a failed check's VM kept without `--keep`, deleted at the branch's next check or by `clean`, a VM's disk being the cost. About a batch (inferred).
- **Merge fitness in status.** For a pull request's ports: their maintainers and openmaintainer, when MacPorts' 72-hour maintainer timeout passes, who was notified (the description's mentions), and who approved, so the person sees when it may be merged. About a batch.
- **ports.macports.org's build history in `impact`, and beside a baseline** (asked by two sessions, exiv2's and bazel's). Whether a port built on each release on MacPorts' builders before, to choose between a patch and `known_fail`, the site's API read once a port; a failed check's offer of a baseline says it too, where the builders failed the same release, which can save the baseline's build. About half a batch, a candidate when the next release is sized.
- **Another open pull request said when work starts.** submit looks for other open pull requests for a port and ticks the template's box only where it looked (forge/github pullrequests.go, prdescription.go); `start` and `update` would say one at once, before the work, where the bazel session ran `gh pr list` by hand. About a quarter batch.

Left from v0.6.0's and the docs dig's work:

- **GraphQL's batched tag and release lookups across repositories,** Then's item 5's rest: its conditional requests (0f6e9a39), a look past the hour's allowance asking first (52cb54c9), and pull requests read in one GraphQL request (c719fff1) are done, and a look's tag and release lookups are still one REST request a repository.
- **A worked command-provider example building on another Mac over SSH** (the dig's I27), held, not declined: it waits for barracuda156's answer on herbygillot/dockhand#5, and would be a tested tool, not a doc.

### Taken when their area is next touched

Codes: the architecture review's A-H1 and on, Codex's review of 832f059e's CX-1 to CX-12, the review of 5897a82's R3-1 to R3-40, and [Codex's review of 8c007979](reviews/2026-10-22-architecture-8c007979.md)'s C8-1 to C8-10; the other reviews and the triage notes are at tag `docs-archive-2026-10-25`. Also when touched: A-H3 to A-H7, A-H10, A-H11, M4, and C4a.

- **A link notice names the dependency whose pkg-config brought a library in** (the gnome-voice session's note 9, inferred, 2026-10-24; when the link notice is next touched). Its check named cairo, pango, harfbuzz, gdk-pixbuf2, graphene, and gettext-runtime as linked and undeclared; gtk4's pkg-config Libs carry those through its public Requires, and glib's carries libintl (inferred, not read on the guest). The notice is right that the binary links them, and a port's dependents are found through what it declares. It would add, where a declared dependency's .pc file names the library, "through gtk4's pkg-config Libs", so the person can choose between declaring it and `-Wl,-dead_strip_dylibs`.
- **Tart workarounds to retire as Tart releases fixes:**
  - the retry of a listing that raced a delete ([openai/tart#1353](https://github.com/openai/tart/issues/1353));
  - trusting a delete only by the VM's absence (#1345, fixed by #1350 on 2026-09-26, hours after 2.39.0 was tagged: unreleased as of 2026-09-27);
  - guests reached over SSH, never `tart exec` (#1346); setup's agent readiness probe is the last `tart exec`, and could move to SSH.
- **The code-organization review's smaller findings,** each when its files are next touched. Findings are the review's, as the 9604d9f5 corrects them.
  - **Serve:** its workers each say a problem once, and its daily look runs off the loop (finding 3).
  - **Copies to fold:**
    - one table test for exec admission (finding 15);
    - `forge/github`'s guards (finding 16);
  - **Structure:**
    - `macports`' program and platform mechanics in a subpackage (finding 21);
- **From Codex's architecture review** (CX; the triage (8ac426e2)):
  - CX-8, the version probe below portedit (outdated builds a `portedit.Service` to probe, and quiets its "editing"), only if a read-only consumer still needs it;
  - the file split: sourcecompare/compare.go's CMake wording into a file of its own when CMake comparison is next touched, and assess's files split by concern when assess is (Codex's review of 8e314b96).
- **From the architecture review of 5897a82** (R3-40; the triage (237cc5db)): smaller placements below the engine, depblock's `ConfirmSource`, distfetch's `notFoundWords`, portedit/correction.go reading `Files[0]`, a Tart provider helper (buildenv/tart/provider.go), and `HostMacPorts` read from three paths.
- **From Codex's architecture review of 8c007979** (C8; 8bc1f6c8):
  - C8-8, sourcecompare's `Compare` reporting each option's change with nothing of the Portfile, assess attaching whether the Portfile sets it (its `named` and `sets` callbacks today), with compare.go's split above;
  - configuration's provenance carried from the parse, where config.go derives "default" from a value's emptiness, so an explicit false reads as default;
  - compose's Tart lookup reading the invocation's PATH, where it falls back to `exec.LookPath`;
  - the two ship drivers, `shipOne` (command/ship) and `Engine.ship` (serve's), which share part 4.1's gate but not a driver: R3-5's residual, when ship is next touched;
- tart/provision's agent plist moves onto macos.LaunchdPlist when tart.SetupProtocol is next raised (R3-21's second half; moving it now changes what images hold).
- **patchcheck's line names each failed hunk's line,** as `patch` says it ("Hunk #1 FAILED at 29"), beside the count it gives now, when patchcheck is next touched (field testing's e2fsprogs 1.47.4 and net-snmp 5.9.5.2 bumps, 2026-10-19, where bump named each patch that doesn't apply before its check; the roadmap's history).
- **The observer's boundary at Golden Gate.** A boundary at `${os.major} >= 27` samples nothing below it, since its lower neighbor, Darwin 26, never shipped; the release below, 25, should stand in (7f593240).
- **What batches 14 and 15 left** (batch 14 (2030b626), batch 15 (31101443)):
  - the mirror's index seeding a base older than it, by the paths changed from the base to a commit after the mirror's time. Sound, but a `_resources` change in that span forces a full pass anyway, and in this checkout a base a week old has one: it would spare macOS 15's 3m42s full pass only for a base a few days old, once per base and release (check-64);
  - serve remembering, for the life of the process, the builds GitHub has, rather than asking once per candidate it plans.
- **What batches 26 and 28 left** (batch 26 (ae528f06), batch 28 (5d160a88)):
  - `create` on a branch it didn't start, without `--new`, still finds a taken name by its directory alone, not a subport's; and the first `create --new` on a fresh machine may index master to look for a subport, which can take minutes;

## Maintenance

- **MacPorts compatibility.** Evaluator tests pass on Base 2.12.6, and CI runs them against the package of whichever release MacPorts currently points its users at, and a weekly job runs them against base's master (231f48c1). Version adapters follow the Base design prospective (2026-09-24): the current release plus a pinned master preview. Keep Base and PortGroup compatibility distinct ([evidence](macports-compatibility.md)).
- **Images.** Twelve images are made and checked: base and Xcode for macOS 12, 13, 14, 15, 26, and 27. Re-probe the facts table when an image is rebuilt; the 2026-09-27 probes found every earlier row unchanged. Golden Gate needs Tart 2.39.0 or newer.
- **Package boundaries.** Extend import checks when touching a boundary that matters; the command and engine boundary tests are the models. Don't split packages by size alone. CI runs `make deadcode`, which fails on a function nothing reaches, tests included. Keep test-only exports documented as such. After a milestone, a sweep runs the tool without `-test`: what it lists, only tests reach, and each is test support, documented as such, or dead. The tool can't see an unused exported method on a type that reaches an interface or reflection, so a sweep checks those by references too.
- **Relocations.** A batch that moves a rule to its owner lists, in its note, the call sites it switched to the owner's rule, and closes with a grep across internal/ for the old rule's shape, its predicate, sentence, or switch, deleting what it finds or saying why it stays. Each of the 5897a82 review's high findings would have been caught by that grep on the day (its section 5). A rule is moved only once every consumer carries the facts it needs and stops judging beside it: findings 3, 4, 6, and 9 of Codex's review of 8e314b96 are a consumer judging beside its owner, so a move adds an agreement test across the consumers, check, review, status, submit, ship, and serve, where they apply. The engine was 24,704 lines in 68 files at 8e314b96; it's measured again at v0.7.0's rc1, and not split for size alone.
- **Dependencies.** Minor releases go on main when they build and the gate passes, as 3706d086's did; a major version is measured first, by building main against it, and placed here like any fix (the person's "Let's go with you recommendation.", 2026-10-24 18:31Z). The library survey's preference for no new dependencies stands.
- **GitHub's budget.** Requests to its API are paced at 750 a minute, under its documented 900. A whole `outdated --mine` spends about 1,800 of the 5,000 an hour the person's account has, a budget shared with the GitHub CLI.
- **Test throughput.** The cost unit is the MacPorts interpreter. Keep per-test timing visible, and prefer simulated clocks for time-driven tests; v2's `workflow` tests show what wall-clock budgets do under load.
- **The test launcher.** The Makefile still shards the command package's tests as unable to run in parallel, which X1 ended on 2026-10-21 (Codex's review of 8c007979's cleanup table): re-measure and drop the shard launcher, any time.
