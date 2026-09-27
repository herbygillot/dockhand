# 2026-09-26: a real update of five ports

The roadmap's step 7 waits for v3's path to work end to end on real work.
So five of the person's ports with newer releases were updated with v3:

- broot and nushell, Rust ports whose crate lists move with the version;
- miller, trufflehog, and prometheus, Go ports whose vendored modules do.

It ran in a scratch clone and database, with master fetched from GitHub's
macports/macports-ports (`1c84679`), and nothing pushed. What broke is
fixed here, each with a test.

## `outdated`

`outdated --all` found all five updatable:

| Port | Now | Newest |
|---|---|---|
| broot | 1.60.1 | 1.60.2 |
| miller | 6.21.0 | 6.22.0 |
| nushell | 0.115.1 | 0.116.0 |
| prometheus | 3.14.0 | 3.15.0 |
| trufflehog | 3.97.6 | 3.97.9 |

It took two minutes, 96 CPU seconds, for five ports, and as long again
when repeated. Measured afterwards:

- **Not the index or the evaluator.** One port at an unchanged master took
  35 seconds, 28 of them CPU, with no new index generation. Planning
  evaluates broot in half a second, and plain MacPorts in 0.08.
- **The cost is upstream discovery.** It has MacPorts evaluate the
  Portfile once for every candidate release tag, to learn the version each
  would give. broot has hundreds of tags, and each evaluation carries its
  555-crate list, so its `tclsh` spent about 24 CPU seconds.
- **The likely fix** is to evaluate candidates newest first and stop at
  the first acceptable one. It's recorded here to do on its own, with its
  own tests. Each run also fetches the newest master, and MacPorts' master
  moves every few minutes, so a new index generation is built most times.

## Fixed

- **`update --outdated` refused more than two ports.** Its help says it
  updates every port named, but the argument check was the single update's,
  a port and a version. It now takes any number with `--outdated`.
- **`update --outdated --plan` ignored `--plan`.** It showed the split and
  then asked to go ahead. It now shows the split and starts nothing.

## The update

`update --outdated --yes` made one branch each from fresh master, each
update tidied into one commit named in MacPorts' style ("broot: update to
1.60.2"):

- **The Go ports** (miller, trufflehog, prometheus) fetch their modules at
  build time (`go.offline_build no`), so only the version and checksums
  moved.
  - miller's and prometheus's `go.toolchain_min` went from `1.25.0` to
    `1.26`, the series their go.mod now requires. That is the Go toolchain
    PortGroup's unit, and MacPorts has `go-1.26`.
  - The upstream comparison listed each go.mod change. prometheus's two
    "adds" are the Azure SDK's `armcompute` and `armnetwork` moving to v8,
    with v5 and v4 dropped.
- **The Rust ports** (broot, nushell) had their crate lists rewritten:
  broot from 571 crates to 555, and nushell from 991 to 983.
- **`submit` had no `--plan`**, though Design v3 says every authoring
  command takes one. Its only preview was running it without a terminal
  or `--yes`, which shows the plan and then refuses. That is an accident of
  prompting, not a promise, and the session's permission rules rightly
  wouldn't run it against the person's fork. `submit --plan` now shows the
  preview and changes nothing, here or on GitHub. It refuses `--check`,
  `--passing`, `--yes`, and `--ready`.

## Checks

Each branch's commit was checked on Tart, on Tahoe with Xcode, in a fresh
clone of `dockhand-xcode-tahoe`:

| Port | Result | Time |
|---|---|---|
| miller | passed | 2 min |
| trufflehog | passed | 3 min |
| prometheus | passed | 5 min |
| broot | passed | 6 min |
| nushell | passed | 17 min |

Each built from source:

- **miller:** its logs show go-1.27 from a binary archive, 33 modules
  downloaded, and `go build ./cmd/mlr`.
- **nushell:** 1,732 crates compiled, and cargo's release build took 4½
  minutes.

No clone was left behind.

## Submission: not done, waiting on the person

The session's permission rules stopped `submit` from running against the
person's fork. A fork remote had been added to the scratch clone for the
preview, and was removed. The branches, commits, and passing checks are in
the scratch clone and database, ready for the person to decide how to
submit.
- **The pull request's Tested on read "macOS 25 arm64".** That is Tahoe's
  Darwin version, 25, presented as macOS's. It also said "Developer tools
  not recorded", though the guest had read its macOS, Xcode, and tools.
  This was caught in miller's preview, before anything was submitted.
  - What an environment reports about itself is now kept with its
    execution (`model.Observed`, schema 12, never overwritten once
    reported), through a new `Build.Observe`.
  - The Tart guest also records Xcode's build.
  - Tested on now states it in the template's form: "macOS 26.6.2 25G71
    arm64", then "Xcode 26.6 17F42", then who built it. Without a report,
    it names the release ("macOS 26 (Tahoe) arm64") and the tools the
    environment stated.
  - The table's columns, and submit's preview, use the same words as
    everywhere else: "tart macOS 26 (Tahoe) arm64 with Xcode".
  - A release dockhand doesn't know is described as "Darwin 30", not
    "macOS 30". The submit test's "Tahoe" fixture had used Darwin 26, and
    passed only because of that same mix-up; it now uses Darwin 25.
- **Submit's preview didn't say whether a resubmission changes the
  description.** Refreshing miller's pull request said only "updates
  #34951", so what would change had to be compared by hand. The PR line now
  says one of:
  - "its description is current";
  - "refreshes its description from Tested on down";
  - "its description is yours, and stays as it is", as before.
- **prometheus's recheck lost what its guest reported.** Its preview read
  "Xcode, its version not recorded", while trufflehog's, just before, had
  it all.
  - The guest wrote its facts only with its first result, and a
    single-target check writes that just before "finished".
  - When the provider read "running" and then found the program already
    gone, it read the last results and recorded the targets, but not the
    facts. prometheus finished in that window, and trufflehog didn't.
  - Every read now goes through one path, the last one included.
  - The guest also writes its facts as soon as it has gathered them.
  - A test reproduces the window, and fails on the old code.
- **`.DS_Store` made every build "dirty".** Go marks a build modified
  when `git status --porcelain` shows anything, untracked files included.
  So Finder's `.DS_Store` files stamped a `+dirty` version into the
  pull requests' signature, until they were built from a clean worktree.
  `.gitignore` now ignores `.DS_Store`.

## `outdated` evaluates only the newest tags

Discovery from tags had MacPorts evaluate the Portfile once for every
eligible tag, to learn the version each would give. That is correct,
since a Portfile computes its version from the tag in Tcl, but it costs
one evaluation per tag.

Now the tags are grouped by their captured version, newest first, by
MacPorts' own `vercmp`:

- **The newest group is evaluated, and the next.** That checks, at the one
  place it decides the answer, that the Portfile's versions follow its
  captures, as stripping a prefix or swapping separators does.
- **If the next group ties or leads**, every tag is evaluated as before,
  so a Portfile that orders its versions otherwise gets the same answer.
- **Consistent with the other paths.** Discovery from a livecheck or a
  listing already chose from the captures and evaluated only the winner.
- **What's given up:** a failed evaluation still surfaces, but only for
  tags that are evaluated. A tag that can't be the newest is never tried.

Tests:

- sixty tags evaluated twice;
- a Portfile whose versions run against its tags, below the top two:
  every tag evaluated, each once, and the true newest chosen;
- the existing ordering, tie, and batch tests.

Measured: `outdated` for broot, nushell, and miller, all current, took 12½
seconds, 3 of them CPU. Before, broot alone took 35 seconds, 28 of them CPU.
By then all five pull requests had been merged, so master had the new
versions and every port read "current".

## Tied tags at one commit

The person settled what discovery does when two tags tie as the newest
version: if both point at one commit, take the tag in the most recent tag
style.

- **Where ties come from.** A port's tag pattern fixes its prefix and
  suffix, so a tie comes from spellings MacPorts' `vercmp` counts equal:
  - `1.2` and `1-2`;
  - `1.02` and `1.2`;
  - for a port with no fixed prefix whose Portfile strips a `v`, `v1.2`
    and `1.2`.
- **A tag's style** is its spelling apart from its numbers: `v1.2` is
  `v#.#`, and `1-2` is `#-#`. Tags carry no dates, so "most recent" is
  read from the releases.
- **The rule, in order:**
  1. Tied tags at different commits are different releases, and still
     need naming.
  2. At one commit, the project's other tags are read from the newest
     version down. The first release tagged in just one of the tied
     styles decides.
  3. If none does, because every release is tagged both ways, the style
     of the tag the port follows now decides.
  4. Otherwise, as before, dockhand asks for the tag.
- **Tests:** dots lately, dashes lately, both lately so the port's own
  style, and two releases refused.

## The two-port branch: flatbuffers and libsigmf

`outdated --mine` over all the person's ports took 25½ minutes, 6 of them
CPU:

| Checked | Outdated | Current | Couldn't check |
|---|---|---|---|
| 1,076 | 180 | 729 | 167 |

Most that couldn't be checked use a Portfile convention automatic
discovery doesn't take. Crossing the outdated ports with the port index's
dependencies found three pairs:

- **netdata and libuv**, but libuv has 20 dependents and is far behind;
- **vapoursynth and av1an**, but av1an's Portfile notes it broke on
  vapoursynth 73;
- **flatbuffers, 24.3.25 → 25.12.19, whose one library dependent is
  libsigmf.** libsigmf regenerates its flatbuffers headers at build time,
  so a flatbuffers release can break it. This was chosen.

The path:

- **`update flatbuffers --revbump-dependents`** updated the version and
  checksums and bumped libsigmf's revision. It warned that upstream's
  `CMakeLists.txt` had changed.
- **`tidy`** made two commits: "flatbuffers: update to 25.12.19" and
  "libsigmf: rebuild for flatbuffers 25.12.19".

Noted on the way:

- **The five merged commits carry a `+dirty` Generated-By trailer.** That
  is `…-583124970cca+dirty`, from the morning's build, when `.DS_Store`
  made every build dirty. It is permanent in MacPorts' history, and
  harmless.
- **This branch's commits were remade instead.** `restore tidy-6`, then
  `tidy` again with a clean build, gave `…-8d7a84811772` with no
  `+dirty`.
- **`update --new --plan` refuses,** since a plan starts no branch.
  Previewing an update before starting work is natural, so it should plan
  against fresh master instead. To do.
- **Probable false positives among the outdated:** dolt → `040.15`,
  bat-extras → `20200408`, and certgraph → `20220513.20220514.0`. Old,
  oddly spelled tags win on `vercmp`'s numbers. To do.
- **`check --on tart:tahoe,sequoia` passed all four builds** (check-17):
  flatbuffers, then libsigmf, on each release. libsigmf's log shows
  flatbuffers 25.12.19, built earlier in the same guest, activated before
  libsigmf was built against it.
- **`submit`'s preview** lists both commits in the description's table,
  and both ports on both releases under Tested on. It rightly stops
  until the person gives a title, since the branch changes two ports.

## Old tags that compare newer are set aside

`outdated --mine` found three probable false positives. Their tags show why:

- **dolt** has a stray `v040.15`, a mistyped `v0.40.15`, and `vercmp`
  reads 40 as newer than 1.81.4;
- **bat-extras** has old undotted date tags (`v20200408`) from before
  `v2024.08.24`, and 20200408 beats 2024;
- **certgraph** has an odd `v20220513.20220514.0` beside `v0.1.2`.

**The guard is time:** a newer release never predates the one the port
has.

- When the chosen tag's commit was made before the commit of the tag the
  port follows now, the tag is set aside and the next newest is chosen.
  The result says so: "set aside v20200408, older than v2024.08.24 though
  it compares newer".
- A new tag on the same commit is still newer.
- Where a forge can't date commits, or the port's own tag isn't there,
  the guard stands aside rather than blocking.
- **Commit dates** come from a new optional `forge.DatedRepository`: the
  committer's date, from GitHub's and GitLab's commit APIs.
- **It costs API calls only when an update is found:** the two tags, and
  their commits.

**Tests:** a bat-extras-like history reads current, with the old tag set
aside, and a genuinely newer release beyond it is still found. It fails
with the guard disabled.

**Live, against GitHub:**
- bat-extras and certgraph are current;
- dolt is 1.81.4 → 2.3.5, its real latest release, from 2026-09-16.

## `update --new --plan` looks before starting a branch

It refused ("--plan changes nothing, so it starts no branch"), though
previewing an update before starting work is the natural first step.

- **A version update's plan with `--new`** is now prepared on master as
  fetched now, through the clone, since a plan only reads, and starts no
  branch. It says so: "Planned on master 444d8ea (fetched just now);
  --new without --plan starts the branch".
- **`--revbump-dependents`** lists the dependents from the index at that
  master. With no branch, none is "already changed by the branch".
- **Checksum refreshes and revision bumps still need a branch.** A
  stealth-update check compares against the branch's base.
- **Live:** `update dolt --new --plan` showed 1.81.4_1 → 2.3.5 on master
  `444d8ea`, and no branch was made.
