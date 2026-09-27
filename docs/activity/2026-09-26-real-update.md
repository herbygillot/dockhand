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
