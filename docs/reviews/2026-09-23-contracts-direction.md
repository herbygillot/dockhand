# 2026-09-23: the contracts review, walked through (in progress)

Draft, not checked in. A running record of the walk through the
[contracts review](2026-09-23-contracts-review.md) and its
[inventory](2026-09-23-contracts-inventory.md): where the review is wrong
against the code, what was decided, what is proposed and waiting, and what
the walk found along the way. When the walk ends this settles into the
roadmap as one reconciled item and the design documents it changes.

## Corrections to the review

- **3.3 says "the prepared edit is checked on one platform too". It is
  not.** Preparation observes every platform context the Portfile
  distinguishes: `observe.Session.Profiles`
  (`internal/macports/portedit/observe/platform_observations.go:19`)
  scans baseline and candidate for Darwin-major, architecture, and
  operand boundaries, including unexecuted branches, and preparation
  requires archive and checksum coverage in each
  (`portedit/version.go:88`, `checksums.go:27`, `artifact_plan.go`).
  Only Tart verification is host-bound.
- **Two mechanisms share the word "modeled".** `eval.Model`
  (`2cb56428`, 2026-09-22) makes a non-Mac host describe a Mac; every
  observation it makes is modeled and Tart setup refuses it.
  `ObservationRequest.Platform` with `Modeled` (2026-09-15, "plan scoped
  updates across archive contexts") is preparation observing other
  platforms on any host. Deno's per-architecture archives and Wasmer's
  Darwin 22 guard are the second.
- **3.5's premise falls with 3.3.** It argued the grammar's all-branch
  judgment "buys nothing while verification is host-platform only".
  Preparation needs other platforms' fetch inputs, and GitHub builds the
  fork's whole matrix. The grammar's real advantage is different again:
  it judges branches that depend on host state, which differ between
  dockhand's host and the builder. A run witnesses the host's path only.
- **3.1 is not an unconsidered bound.** `roadmap.md:17` records the
  multi-target contribution, one commit per port directory in dependency
  order with one PR, as agreed on 2026-09-21 and not yet built.
- **The optional-capability tests** the review proposes in its body (one
  test per implementation asserting the capabilities it satisfies) are
  missing from its recommendations list.

## Decided

1. **Preparation models every platform context the Portfile
   distinguishes** (OS version, architecture, operands bound to either)
   so every checksum a correct update needs is edited; Deno is the
   reference case. A context it cannot observe without host state stops
   preparation as inconclusive.
2. **Evaluation should not harm the host it runs on.** Dockhand may
   develop facilities to contain, refuse, or answer for a Portfile's side
   effects so that more Portfiles can be processed safely. The direction
   is "MacPorts in a controlled room", below.
3. **Verification policy is per provider.** GitHub keeps the fork's
   runner matrix; dockhand does not change it.
4. **Tart builds on the host's macOS release by default.** `--os` adds
   other releases. Confirmed after the Golden Gate test showed Tart can
   run newer guests: the host's release is the platform preparation
   evaluates natively, so the default build needs no split between
   evaluation and build platform.
5. **Dockhand does not gate a newer OS.** Tart is the one to refuse what
   it cannot run. The refusals in `app/setup.go:66` and
   `verify/tart/config.go:78` go. A Tart failure to boot is an
   environment failure, never a failed build of the port. A release
   dockhand has no name for remains a missing fact, answered with
   `--source` or `--image`.
6. **An `--os` release with no provisioned image is refused with the
   missing image named and the command to provision it**:
   `dockhand setup --os <slug>`, with `--xcode` when that release's
   evaluation of the port needs full Xcode.
7. **A build on a release other than the host's needs the evaluation
   platform and the build platform recorded separately** in the accepted
   spec. The build platform must fall inside what preparation covered,
   and each release's image choice comes from that release's modeled
   `use_xcode`, not the native one.
8. **"MacPorts in a controlled room" goes forward, inventory first.**
   Below.
9. **Each macOS release gets a facts table**, and a modeled context
   answers toolchain questions from it rather than from the host: Xcode
   and Command Line Tools versions, developer directory, which compilers
   exist and their versions, SDK paths and versions. Replaces
   `ModelVariables`' premise that "a Mac never needs this: its own tools
   answer for every platform it models". The Darwin table in
   `internal/macos/release.go` is where it belongs.
10. **The facts table is checked in and harvested from real images.**
    Preparation models releases it has no image for, and must stay fast,
    so it reads the table; the facts in it are gathered by probing
    provisioned macOS images and then checked in. Local verification,
    where an image exists, builds against the image's real facts.
    Proposed with it: the guest records the facts the table covers so
    each verification reports drift against the table without changing
    the verdict; the table records what a fresh `setup` installs, dated;
    hardware facts (`sysctl`, `uname -m`) follow from the modeled platform
    in `PlatformVariables`. *Amended by 12*: releases and architectures
    Tart cannot boot take their facts from MacPorts' buildbot logs from
    Darwin 10, and Base's own tables only for Darwin 8 and 9.
11. **The Tart guest runs what MacPorts CI runs, in its order.**
    `macports-ports/.github/workflows/main.yml`: `port lint` (fails the
    job), `mpbb install-dependencies` (fails the job), `mpbb install-port`
    (`fetch`, `checksum`, `install -dkn --unrequested`; fails the job),
    then `mpbb test-port` after install, advisory ("not setting fail=1 as
    a 100% passing test suite is not considered…"). So the guest installs
    and activates the dependencies in their own command before the target
    is evaluated for its fetch, and moves its test step after install. The
    build stays the gate, lint and install decide with it, tests are
    advisory as in CI, and `--tests required` remains the explicit way to
    make them decisive. The verifier digest changes, so existing evidence
    stops being reusable; the verifier protocol version (3.2) would soften
    that for later changes. *Amended by 22*: the guest runs this sequence
    for each target of a changeset in turn, deactivating everything and
    activating only that target's dependencies before each.
12. **The facts table is keyed by Darwin, architecture, and profile, and
    each entry records its source and date.** Sources, in order of
    preference: facts harvested from Tart images wherever dockhand can boot
    one (arm64, Darwin 21 and later, in dockhand's Command Line Tools and
    Xcode profiles), which win over the buildbots for those environments;
    MacPorts' buildbot logs for every other release and architecture from
    Darwin 10 (all x86_64 and i386, and arm64 releases Tart cannot boot);
    Base's own tables for Darwin 8 and 9, which no builder covers. Both
    harvesters are checked in as developer tools, so the table is
    regenerated rather than hand-edited. Where both sources exist the
    buildbot entry is kept as a cross-check.
13. **Setup installs a pinned Command Line Tools generation per release.**
    The facts table holds one generation (the major version) per release;
    setup installs the newest tools of that generation Software Update
    offers, and refuses, naming what is offered, when none is, never
    falling back to the newest. Replaces `internal/macos/install.go:41`'s
    `sort | tail -1`, which gave both Tahoe images the macOS 27 tools.
    The generation follows MacPorts' GitHub CI pin where CI covers the
    release (`.github/workflows/bootstrap.sh`: Xcode 16.2 on Darwin 23,
    26.4 on Darwin 25, set by hand) and the buildbots elsewhere, checked
    by hand when the table is regenerated and recorded with its source;
    dockhand does not parse the CI script. A generation drifts about once
    a year and only for the newest releases; the verification drift report
    (decision 10) flags a guest whose tools differ. MacPorts itself pins no
    Command Line Tools version: mpbb uses what the machine has, the CI
    runners' tools come from GitHub's images, and the buildbots are kept
    by hand. `macos.CurrentToolchain` comes from the table. Tahoe's images
    are rebuilt on the 26 generation.
14. **Several `--os` mean every one must pass.** One job with a target per
    release; publication requires each requested release to pass, as it
    requires the whole dependents cohort (`policy/coverage.go:15`); the PR
    body lists each release's result. `--target-image`'s "this is not a
    platform matrix" stops being true. *Amended by 22*: one guest per
    changeset per release.
15. **Dockhand gets a configuration file, `~/.dockhand/config.toml`**, and
    `tart.os` is its first key. TOML because `github.com/BurntSushi/toml`
    is already a direct dependency (Cargo manifests). Proposed with it:
    precedence flags, then environment, then the file, then defaults, the
    same order the existing environment variables already follow; a list
    flag such as `--os` replaces the file's list rather than adding to it;
    an unknown key or a malformed value is refused with the key named,
    never ignored; reading the file creates nothing, and help, completion,
    and previews still create no files; values are resolved at intake and
    frozen into the accepted job like every other input, so editing the
    file never changes queued work; `DOCKHAND_CONFIG` names another file
    for tests. The keys that already have environment variables (tree,
    prefix, database, Tart, git) can move into the file over time; nothing
    else is added without a use.
16. **Portfile evaluation is independent of the host's own state; this is
    the highest priority of the evaluation work.** The installation is
    modeled as MacPorts CI sees it at its second evaluation, in native and
    modeled contexts alike, and extra evaluation passes are an accepted
    cost. Two passes, as CI does: the first with nothing installed gives
    the dependencies; the second, whose fetch plan counts, has the
    dependency closure active. No fixed point: dependencies the second
    pass adds are installed after it and never affect it. Answers:
    registry questions from the closure (resolved recursively through the
    index at the source being prepared, default variants); Base's own
    files and programs present, tied to the Base version; the evaluator
    launched with Base's kept environment only (`keepenvkeys`,
    `macports.tcl:1720`) at a builder's values, so no `JAVA_HOME`,
    `DYLD_*`, or proxies from the person's shell, matching the Tart guest
    (GitHub's runners set `JAVA_HOME`; it reaches no fetch input); paths
    outside the prefix from the facts table; any other prefix path's
    existence evaluated present and absent, conclusive only when the fetch
    inputs (`macports.AffectsFetch` and `portfetch::checkfiles`) agree,
    with one consistent answer per path per run, at most three such paths
    (eight evaluations) before the context is inconclusive; reading a
    prefix path's contents or a prefix program's output inconclusive. The
    second pass runs only when the first asked about the installation
    (about 2,000 ports). Pass-two dependencies and configure arguments stay
    estimates. No table of which port owns which path unless the survey's
    inconclusive count justifies rules for systematic families.
17. **Pre-fetch hooks stay judged statically; the review's recommendation
    4, running the hook in the evaluator, is dropped.** The oracle removes
    the safety argument and the closed context mostly removes the host's
    path, but the question is still over every branch the builder can
    take, which one run cannot show, and branches after dependencies are
    installed would multiply evaluations; static judgment needs no values
    (the Java hook's `java_home -V` output only lands in a variable the
    fetch never reads) and costs nothing. `bump-coverage.md:29` keeps its
    rule with that reason in place of "hooks are dangerous". Running the
    hook inside the oracle stays a possible cross-check if refusals remain
    once the oracle exists.
18. **Programs built into macOS are profiled per release in the facts
    table, and the Java gap closes with that profile.** A program macOS
    installs (on the sealed, SIP-restricted system, as
    `/usr/libexec/java_home` is) has its presence and its fixed behavior
    (`sw_vers`, `uname -m`, `machine`, `sysctl` keys, `xcrun
    --show-sdk-path`, `java_home` with nothing installed: "Unable to locate
    a Java Runtime", exit 1) harvested per release; behavior that depends
    on installed state (`java_home` once a JDK may be present) is answered
    by the oracle trying both outcomes the entry names, conclusive when the
    fetch inputs agree. Per release because what macOS ships changes
    (`/usr/bin/python` went in 12.3; `/usr/bin/java` and the tools' shims
    are installer stubs). The guard admits an `exec` of such a program
    whose output only lands in a variable the effect rule already judges,
    and judges a computed option name by its fixed family prefix
    (`depends_${deptype}-append` is a dependency write). Expected to admit
    the 180 Java ports, confirmed by the survey before it is claimed;
    settles the roadmap's Java `exec` question. `bun`, a MacPorts port in
    the prefix, stays refused. The eight Go ports (toolchain check on
    gitlab.com) and five R ports (unbraced condition) are ordinary guard
    fixes.
19. **A port may have several open contributions, each started
    deliberately.** `bump --new` starts another contribution for a port
    that has one. When a port has several, the person says which one
    (`--change`, `--branch`), and every path that would continue "the
    port's contribution" (`bump` without `--new`, `serve`, automatic
    bumps) stops with the ambiguity error rather than choosing. The store
    already allows this (its only constraint is one open contribution per
    branch, `migrations/001.sql:16`) and selection already names the
    candidates (`workflow/contribution_select.go:64`). Replaces
    `resolution-design.md:326`'s "one open contribution per port" and
    `cli-design.md:117`'s "a port with an open contribution cannot adopt a
    second branch"; `target-workflow.md:60` already says this. **`adopt`
    creates a new contribution by default**, with no flag: tracking a
    branch dockhand does not know is new work by the verb's meaning, and
    replacing a contribution's branch is `amend` or `reassociate`. The
    one-step `--adopt` on `verify`, `publish`, and `bump` behaves the same.
    `bump` continues by default and needs `--new`, because "update this
    port" reads as continuing. Guards that remain: the same branch twice
    is refused by the store (one open contribution per branch); a pull
    request already tracked is refused (`migrations/006.sql:5`) naming
    the contribution that has it. `adopt` reports when the port now has
    several open contributions, names them, and points to `amend` in case
    a replacement was meant; `--dry-run` and `publish` say the same.
    `status` shows each contribution by branch and pull request. Two
    contributions editing one Portfile mean the second to merge needs
    `dockhand rebase`; that is the person's call. *Amended by 22*: "the
    port's contribution" is the changesets whose diff touches its
    directory. *Narrowed by 26*: implicit continuation only onto the
    person's own open update of that port.
20. **Multi-directory contributions: the commit rule, and failures
    attributed by a baseline.** MacPorts' tooling ignores commits: its
    pull request CI takes `git diff --name-only macports/master...@`
    (Portfiles and `files/`), expands and orders the subports with `mpbb
    list-subports` (`sort-with-subports.tcl`, dependencies first), and
    builds each in turn, continuing past a failure and failing the check
    at the end; the buildbots' watcher does the same per push and
    triggers one builder run per subport, each passing or failing alone.
    So: **each port directory is changed by exactly one commit, and a
    commit may change several**; a directory changed in two commits is
    refused with a pointer to squashing per directory; dockhand's own
    preparations write one commit per directory. `--squash` stays allowed
    on a multi-directory contribution: one commit changing every
    directory satisfies the rule, targets come from the diff against
    master and never from commits, `amend` still has exactly one commit
    per directory to route an edit into, and the unchanged tree keeps its
    evidence. Squashing several port commits says so, suggests `--edit`
    so the subject can name the dependents, and keeps the originals under
    `refs/dockhand/adopted/` as today. **A dependent that fails
    after the change is judged against a baseline**: the dependent built
    at the contribution's master base, old root, same image and settings.
    The same phase and failing package on the baseline means already
    broken (does not block; the PR body says "already failing on master
    before this change" with both results); a passing baseline means the
    change caused it (blocks); a different failure needs the person's
    attention. Only dockhand's own builds are evidence; MacPorts'
    buildbot history for the port may be shown as a labeled hint. **The
    publication rule**: the roots pass, and every dependent passes or has
    a baseline showing the same failure on master; `--strict-dependents`
    requires all to pass. A revision-bumped dependent that is already
    broken keeps its bump. Baselines run only for dependents that failed,
    and are reusable under the ordinary evidence rules. *Amended by 44*: a
    baseline never unblocks publication by itself. *Amended by 22*:
    the commit rule is superseded (commits are the author's); "roots"
    are the substantive targets.
21. **A failed dependent is reported and the person may acknowledge it;
    the baseline is opt-in.** The default adds no build: when a dependent
    fails after the change, dockhand says so and stops before publishing.
    Attached in a terminal it asks ("Dependent X failed at build. Publish
    anyway? [y/N]", the console's confirm-before-acting pattern,
    `output.md:76`); detached, under `serve`, with `--json`, or without a
    terminal, the job needs attention and names `dockhand publish <port>
    --accept-failure X`. An acknowledgement records the dependent, the
    failed result, and the time on the job, binds to that tree (an
    `amend` or `rebase` does not inherit it), and appears in the PR body
    as "dependent X failed at build; acknowledged by the submitter, cause
    not established". `--baseline` (or `verify.baseline = true`) runs the
    decision-20 baseline instead, to establish the cause. Dropping the
    dependent's bump (`--no-revbump X`) prepares again and, while evidence
    binds to the whole tree, verifies everything again. An acknowledgement
    never covers a root: publishing a failed root is what `--unverified`
    is for. *Amended by 22*: acknowledgement covers revision-only targets
    and verified-only dependents, never a substantive target. *Amended by
    44*: revision-only is decided from the source, not metadata.
22. **A contribution is a changeset. Ports are what dockhand prepares;
    changesets are what it verifies and publishes.** This is the frame the
    other decisions sit in; where it changes one, the change is noted
    there.
    - **Derived scope.** A contribution is a branch of commits on a master
      base. Its changed port directories, `_resources` changes, and
      targets are derived from its diff against the base, by MacPorts
      CI's rule: a `Portfile` or `files/` change marks the directory
      (`main.yml`: `git diff --name-only macports/master...@`), and the
      targets are every subport of each changed directory
      (`sort-with-subports.tcl` lines 185–189) less `replaced_by`,
      `known_fail`, dependencies known to fail or cyclic, and unsupported
      architectures, evaluated on the changed tree, in dependency order.
      Commits never enter into it.
    - **Target kinds, derived by evaluation.** Revision-only: evaluated
      metadata identical but for `revision`, and no `files/` change.
      Substantive: anything else.
    - **`--only`, repeatable** (named in decision 23), narrows the
      derived targets; a name outside them is refused with a pointer to
      `--also`; a named subport is built even when CI's rules would
      exclude it, recorded as named. Narrowing is recorded and the PR says
      "not built locally: …".
    - **Selection.** A port name selects every open changeset whose diff
      touches its directory; several need `--change` or `--branch`
      (decision 19). Replaces 3.9's "only the initiating target selects
      a contribution by name". `Change.InitiatingTarget` becomes a label.
    - **Preparation writes into a changeset.** `bump jq` edits jq in the
      changeset touching jq, or starts one when none does; `--new` forces
      a new one; `--change` or `--branch` names the changeset, which is
      also how a port is added to an existing one. Dockhand does not judge
      whether bundled ports belong together. `--revbump-dependents` adds
      revision-only commits to the same changeset.
    - **Commits are the author's.** Dockhand writes one commit per
      directory it prepares. `amend` puts an edit into the most recent
      commit touching that directory, or appends a commit for a newly
      touched one. Merges, root commits, and empty changes stay refused.
      Supersedes decision 20's "each directory changed by exactly one
      commit".
    - **Verification: one guest per changeset per release, one subport at
      a time as CI does.** Lint every target first; then for each target
      in dependency order: deactivate everything, activate exactly its
      dependencies (changed ports from this guest's own builds, unchanged
      ones from binary archives unless a source build was asked for),
      fetch, checksum, install, then test (advisory). Registry queries
      then see exactly what CI's `install-dependencies` leaves ("all
      dependencies (and only dependencies) of the port are active"). The
      root is built once, not once per dependent guest. A target whose
      changed dependency failed is recorded as blocked, not failed. Each
      target's result is reported as it finishes, so an interrupted guest
      keeps the finished ones; a guest protocol change that goes with the
      verifier protocol version. `--keep-failed` keeps the guest when any
      target failed.
    - **Publication.** Substantive targets must pass; revision-only
      targets and verified-only dependents may be acknowledged or
      baselined (decisions 20, 21). The title follows decision 24.
    - **Shared releases** stay a preparation concept (editing sibling
      versions consistently); for verification and publication their
      membership is derived. `record.CoverageIntent` and `SameMembership`
      are re-examined rather than carried over.
    - **`_resources`**: CI builds nothing for a `_resources`-only change;
      dockhand keeps refusing one unless targets are named.
    - **Records**: the single-directory `Scope` becomes a derived set;
      every existing contribution is a one-directory changeset, so the
      migration is mechanical and discards nothing.
    - *Amended by 44*: revision-only from the source; the plan separates
      scope, eligibility, selection, and prerequisites; the guest execution
      has one owner with per-target checkpoints.
    - **Evidence**: whole-tree binding means one amend in a twenty-port
      changeset rebuilds every target, which makes per-target reuse (3.2)
      a necessity; reusing a built target that others depend on also
      needs its binary archive kept on the host and staged into the next
      guest, since dockhand keeps no build archives today.
23. **"Dependents" leaves the model; `--only` and `--also` choose what is
    built.** A revision-bumped dependent is a changed port like any
    other. `--only <subport>` (repeatable) narrows the changed subports
    built, replacing decision 22's `--target`; naming an unchanged port
    is refused with a pointer to `--also`. `--also <port>` (repeatable)
    adds unchanged ports built against the changeset, the assurance
    MacPorts CI never gives (it builds only what changed); extras follow
    decisions 20 and 21 (acknowledge or baseline), stay Tart-only (GitHub
    CI builds only what it sees changed), and are listed in the PR as
    extra coverage. Both combine, are frozen at intake, and the PR lists
    "not built locally: …" and "also built: …". The index's reverse
    dependency lookup becomes two selection shortcuts, both shown by
    `--dry-run`: `--revbump-dependents` (revision-only commits for the
    direct library dependents of the substantive ports) and
    `--also-dependents` (the direct build, library, and runtime
    dependents of the changed ports, as extras). `--dependents` is
    retired. `--target-image` is retired too: one guest per changeset per
    release means one image, the Xcode image when any target needs Xcode,
    else the plain one (the harvest showed `use_xcode` stays 0 on the
    Xcode images for ports that do not ask, so they build with the
    Command Line Tools either way); `--image` overrides for the whole
    changeset. *Amended by 44*: `--only` never excludes a changed
    prerequisite of what it selects.
24. **A changeset keeps its title.** A `--title` given at any step
    (`bump`, `adopt`, adding a port with `--change`) is kept and used at
    publication; `publish --title` replaces it. With one substantive port
    and no title given, the title is that port's commit subject; with
    several and none given, publication asks, and dockhand never composes
    one. Once the PR exists, dockhand changes its title only when
    `--title` is passed, so a maintainer's retitle on the forge is never
    reverted, as an edited body is kept whole.
25. **`bump-revision` starts a new changeset unless `--change` or
    `--branch` names one**; it never edits an existing changeset
    unasked. A revision bump has its own reason, so its own changeset and
    PR. Defaults by verb: `bump` continues the changeset touching the port
    (a newer version supersedes the open one) and `--new` forces a new
    one; `adopt` and `bump-revision` always start new. A port that exists
    only on a branch (a new port) cannot be revision-bumped from master,
    so without `--change` it is refused with a pointer to it; today's
    quiet prepare-onto for that case (`usage.md:84`, `cli-design.md:215`)
    becomes explicit. When the port also has an open update, the new
    changeset is created and the plan names the open one, since the two
    will conflict over `revision` (decision 19).
26. **One continuation rule for the preparation verbs** (supersedes the
    first version of this decision, which had `checksums` always start
    new). A verb continues an open changeset only when that changeset is
    an open update of the same port (the port is a substantive target in
    it), exactly one such exists, and it is the person's own; otherwise
    it starts a new changeset and names what is open. `bump` continues
    under this rule (a newer version supersedes the one in flight; MacPorts
    expects one PR per port update). `checksums` continues under it too
    (its main use is fixing the update in flight after upstream re-tagged
    the release); a stealth fix of master's version while an update is
    open is `--new`, and the notice names both versions when they differ.
    `bump-revision` always starts new (decision 25): continuing would be
    wrong, since an update already rebuilds the port and resets `revision`
    to 0. Narrower than decision 19 as first written: a port that is only
    revision-bumped in another changeset is not continued into it (`bump
    jq` does not join libfoo's changeset); an adopted pull request of
    someone else's is never continued implicitly, since that would push to
    their fork, and `--change` continues it deliberately; several open
    updates of the port are the ambiguity refusal. A port that exists
    only on a branch is refused by `bump-revision` and a fresh `checksums`
    without `--change`.
27. **Revision bumps and subtraction in the changeset model.**
  `--revbump <port>` is dropped: adding revision bumps is `bump-revision
  portA portB … [--change X]`, one request for several ports, sharing its
  required `--subject`. `--revbump-dependents` stays as a shortcut on
  `bump` only (a new version is what changes an ABI): the root's direct
  library dependents, not transitive, from the index at intake, shown by
  `--dry-run`, each commit with the derived subject `<dependent>:
  rebuild for <root> <version>` (`--revbump-subject` rewords). `--except
  <port>` subtracts from whichever shortcut list produced the port
  (`--revbump-dependents` or `--also-dependents`; a port is changed or
  extra, never both). A shared `revision` line is bumped for all its
  subports, relaxing `cli-design.md:172`'s sibling refusal for these
  bumps only, with the siblings listed. A new command, `dockhand drop
  <port> [--change X]`, takes a port out of a changeset: restores its
  directory to the base, removing a commit that touched only it or
  amending one that touched others, as a new revision verified again.
  Flags by command: `--revbump-dependents` and `--except` on `bump`;
  several ports on `bump-revision`; `--change`, `--branch`, `--new` on
  `bump`, `bump-revision`, `checksums`; `--only`, `--also`,
  `--also-dependents`, `--except` on everything that verifies (`verify`,
  `bump`, `bump-revision`, `checksums`, `amend`, `rebase`, `drop`);
  `--title` on `bump`, `bump-revision`, `adopt`, `publish`.
28. **Evidence is reused per target, by the identity of what the build
    actually read; no prediction and no reference table decides reuse.**
    Review 3.2, recommendation 5. Today reuse needs the whole repository
    tree, the image's disk bytes (`tart/image.go`), and a hash of the
    guest's files (`tart/files.go:57`) to be identical, so every rebase,
    every amend in a many-port changeset, every guest edit, and every
    `setup --rebuild` discards evidence. Instead:
    - **Recorded inputs.** The guest records, per target, everything the
      build read that dockhand can name: each activated dependency (name,
      version, revision, variants, and whether built here or installed
      from a binary archive), each tree and `_resources` file its
      evaluation sourced, and each dependency name's resolution to a
      directory. A result is reused when every recorded input is
      unchanged by content identity (Git blob and tree IDs, index lookups
      of the same names in the new tree) in an environment of identical
      origin. The argument is the build tools' one: identical reads mean
      identical control flow, so the same reads and the same result. It
      is exactly as strong as the recording is complete. Evaluation-time
      reads are complete once the oracle exists (every Tcl-level read
      passes its dispatcher; the registry is the recorded activated set);
      until then the whole of `_resources` counts as read, since it
      changed on 20 of the last 60 days on master (62 of 2,994 commits).
      Build-time reads are bounded, not traced: distfiles pinned by the
      Portfile's checksums, `files/` by the directory's tree ID, the
      active dependencies, the toolchain and system by environment
      identity. Accepted and documented gaps, as CI accepts them: a build
      step reading another port's directory, a build fetching over the
      network, a nondeterministic build. The facts table plays no part;
      it serves preparation, and decision 10 reports its drift.
    - **Default on**, replacing the whole-tree rule; `--fresh` stays. A
      rebase keeps a target's result when its inputs did not change,
      which makes `human-corrections.md:9` true.
    - **Confirmed in the guest when one runs anyway**: before skipping a
      target, the guest evaluates it and compares its dependency set and
      sourced files with the record, guarding against recording gaps. The
      host-side identity check decides alone only when nothing needs
      building.
    - **Environment identity by origin**: the vanilla source image's OCI
      digest as pulled, the Command Line Tools and Xcode versions, the
      MacPorts version, and dockhand's setup protocol version. Image bytes
      are kept as provenance. A rebuild from the same source keeps
      evidence; a newer `:latest` or new tools does not.
    - **Build archives are kept** so a reused target that others depend on
      can be installed rather than rebuilt: a passing target's archive is
      copied from the guest's `${prefix}/var/macports/software/<port>/`,
      checked against the sha256 the guest reports, and stored under the
      target's input key; a later guest gets it streamed into its own
      `software/<port>/` before activation, which MacPorts uses without a
      signature check (`portarchivefetch.tcl:315–322` skips the fetch for
      an existing archive; signatures are checked only on download).
      Transfer uses the channel already in use: `tart exec` with standard
      input and output (`verify/tart/native.go:46`), which already streams
      a 100+ MB ports-tree tar into every guest; the host names the file,
      and nothing the guest sends is trusted beyond one checksummed
      stream. *Superseded by decision 38*: transfer is over SSH/SFTP,
      since `tart exec` fails out of macOS 12–15 guests. No `tart run
      --dir` share, not even read-only, as a fallback:
      Tart has open reports of host panics with read-only VirtioFS shares
      (openai/tart#1308) and of read-only shares desyncing on Tahoe
      26.6.2 (#1330). An HTTP archive site is not used either (it would
      need a signing key in every image). Archives are removed by `gc` after the
      changeset is retired; no size cap at first, with `gc` reporting the
      store's size and a configuration key added if it is needed.
    - **A verifier protocol version replaces the content hash**, with a
      test pinning the guest files' current hash: changing them fails the
      test until the author either raises the protocol (behavior changed,
      reuse ends) or updates the pin (wording only, reuse kept).
    - **Tart only.** GitHub's evidence is its workflow run on the pushed
      commit.
    - *Amended by 44*: the record holds negative observations and the
      digest of every archive consumed; evidence validity and archive
      availability are separate facts.
    - **Trace mode** (`port -t`, darwintrace) is set aside: reported buggy,
      unused by CI, and not needed for the guarantee as stated.
29. **No provider fallback; switching providers is explicit
    replacement.** Review 3.6, recommendation 7, closed without
    alternatives in accepted intent. The provider is chosen at intake
    (`--provider auto` already picks GitHub when no Tart image serves the
    request, before acceptance) and at capacity the job waits; nothing
    ever does "this is busy, so that". The rules "a configured provider
    registry never falls back" and "a full Tart queue waits rather than
    switching to GitHub" stay, reworded to give their reasons: a job
    satisfiable by either provider would make "verified" mean two things
    (Tart's per-target evidence with `--only`, `--also`, reuse, and
    baselines against GitHub's matrix on a commit), and upstream's own CI
    builds the pull request once it is open. Switching is the person's:
    `verify <port> --provider <p> --replace` cancels the changeset's
    current verification job, waiting or running, and accepts a new one
    recorded as superseding it, from the same prepared revision, keeping
    the old destination (`--to`) unless another is given; the prepared
    branch and finished evidence are kept, as cancellation keeps them.
    Replacing a running build asks first when attached; detached,
    `--replace` is the consent. Without `--replace`, a verification with a
    different provider for a changeset that has one is refused, naming the
    job and the flag, so two verifications never run side by side
    unasked. A `bump` waiting at its verification stage is replaced the
    same way, continuing from its prepared revision. The provider stays a
    field of the accepted spec, so later features can build on
    replacement. Standing preferences live in the configuration file
    (decision 15) and freeze at intake; lists in intent mean "all of
    these" (decision 14), never "one of these". Build, lint, install, and
    fetch failures are never retried automatically.
30. **Infrastructure failures are retried; verdicts never are.** Three
    attempts in all (the first and two retries), 1 minute before the first
    retry and 5 before the second, through the driver's existing retry
    scheduling (`RetryAt`), so detached jobs and `serve` carry them. Only
    attempts that ended with `record.InfrastructureFailure` (the VM did
    not boot, the agent was lost, cloning failed, a provider error) are
    retried; build, lint, install, and fetch failures, and refusals before
    submission (`ErrImageUnavailable`, `ErrExecutableUnavailable`), are
    not. A retry covers only the targets with no verdict yet; finished
    targets keep their results and archives, so a crash late in a large
    changeset resumes rather than restarts. When the same target was
    mid-build during two infrastructure failures, retrying stops and that
    target needs attention ("the guest failed twice while building X"),
    since a build that exhausts memory or disk looks like infrastructure.
    Each retry is a new attempt in `status`, announced ("retrying after
    infrastructure failure: <detail> (retry 1 of 2)"); after the last the
    job needs attention with all three failures shown. Intermediate failed
    environments are released; `--keep-failed` keeps the last. Not
    configurable at first.
31. **Setup runs `tart run` in its own process group and stops it itself.**
    It keeps the child's handle, stops with `tart stop`, then SIGINT, then
    SIGKILL, so the terminal's Ctrl-C reaches dockhand rather than the VM
    directly. Goes with the ASIF fixes: `cleanup` reports its errors and
    deletes even when stop failed, and a delete is confirmed through
    `tart list` (decision 32), never by `tart delete`'s "does not exist".
32. **Dockhand depends only on interfaces its external components
    officially expose; an unavoidable exception is flagged to the person.**
    A Tart upgrade must not be able to break dockhand silently. Tart's
    documented surface: its CLI (`pull`, `clone`, `set`, `run`, `ip`,
    `stop`, `delete`, `rename`, `list` and `get` with `--format json`,
    `exec` through the guest agent), `TART_HOME` and `TART_NO_AUTO_PRUNE`,
    and the locations `~/.tart/vms/` (local images) and
    `~/.tart/cache/OCIs/` (pulled images) (`faq.md`). Not documented:
    anything inside a VM directory (`config.json`, `disk.img`,
    `nvram.bin`, the `config.json` lock) and a stable schema for the JSON
    output. The `config.json` lock workaround is dropped: dockhand
    declines ASIF images (Golden Gate today) until Tart fixes `list` and
    `get` (reported as openai/tart#1344; `delete` on a running VM saying
    "does not exist" as #1345, both 2026-09-23), naming #1344 in the
    refusal, using `tart get`'s `DiskFormat` on the stopped clone; a
    person's own running ASIF VM breaking `list` is recognized, reported,
    and waited out like capacity. Redesign: existence and running state
    from `list` and `get` only; delete only after `list` shows the VM
    stopped, and confirmed by its absence from `list`, never by `delete`'s
    "does not exist"; image identity by origin (setup resolves the tag to
    a digest through the registry's public API, pulls `@sha256:…`, and
    writes a manifest inside the guest that verification reads in the
    clone through `tart exec`), replacing the host-side hashing of VM
    files and the stamp cache (`verify/tart/image.go`, `state.ImageCache`);
    dockhand's lock files move out of `<TART_HOME>/dockhand/locks/`
    (`tart/lock.go:28`) into dockhand's own directory keyed by the
    canonical Tart home. Tests pin the JSON fields read (`Name`, `Source`,
    `Running`, `State`, `DiskFormat`), the ASIF error, and `delete` on a
    running VM, so an upgrade that changes them fails a test. Tart's FAQ
    also notes stacked ASIF overlays on macOS 27 hosts, so ASIF will
    spread. Decision 31's "confirmed by the directory being gone" is
    replaced by confirmation through `list`. Open, with tests first: the
    host-side recovery-partition edit for Xcode images
    (`tart/provision/vm.go:63`) and the half-finished-clone check
    (`tart/host/machine.go:42`). Flagged exception candidate: MacPorts
    Base internals in the evaluator.
33. **The driver prohibitions are decisions of the first implementation,
    not principles.** Review 3.4, recommendation 1; prose only. In
    `principles.md` and `architecture.md`, "commands do not spawn
    background drivers", "no Unix socket or separate local request
    transport", and "no process registry, singleton residency lock, child
    driver, socket, or operating-system driver service" are reworded as
    decisions, each with its reason and the conditions that would reopen
    it, taken from `instance-coordination.md` (claims that cannot tell a
    dead leader from a slow one, progress another terminal cannot see,
    reservations invisible across database files). A later session-and-
    lease model then extends the principles instead of arguing against
    them.
34. *Not used: the proposed MacPorts Base decision became 39.*
35. **Closures confirmed.** 3.8, one repository per engine, stays as is:
    each engine is bound to one registered checkout (`state.Scoped`),
    `serve` drives one, `gc` alone reaches across. 3.7, the sparse
    workspace, becomes one rule of the oracle rather than separate work:
    the dispatcher every evaluator read passes through (`source`, `file`,
    `glob`, `readdir`, `open`) knows the workspace's scope, so a read
    outside it materializes that path on demand when it is in the tree
    (`EnsurePort`) or refuses the evaluation when it is not; callbacks and
    enumeration are covered because commands, not stack frames, are
    intercepted. 3.9's bounds: one open contribution per port and "only
    the initiating target selects by name" were changed by decisions 19
    and 22; an edited PR body kept whole, `status` never contacting a
    provider or forge, provider configuration compared for reuse (within
    decision 28's key), and one forge per client stay. Two 3.9 items are
    reopened: cleanup (proposed below) and branch naming.
36. **Cleanup is automatic.** Review 3.9, reopened: nothing guarantees a
    person runs `gc`. The cycle's existing bounded pruning (`operations.md`,
    "Routine cleanup") extends to everything `gc` reaches: index
    generations and GitHub log caches unused for 7 days, and build
    archives 7 days after their changeset retires (an open changeset's
    are kept). Kept-failed VMs get a default recorded retention deadline
    of 14 days, with `status` warning two days ahead. A full pass runs at
    most once a day per database, during `serve` or after an attached
    command's own work, never delaying it; a pass runs at once, with a
    warning, when free space on the artifact or Tart volume falls below
    30 GB. Dockhand's own pulled Tart cache entries are removed with `tart
    delete <reference>` after 30 days unused, never a global `tart prune`.
    Thresholds are configuration keys (`cleanup.after`,
    `cleanup.keep_failed`, `cleanup.min_free`), with `cleanup.automatic =
    false` to turn it off. `gc` stays as the explicit, immediate form.
    Open work, live claims, and unfinished jobs are never touched.
    *Amended by 44*: archive cleanup respects every live reference, not
    only the retirement of the changeset that built it.
37. **Changeset branches are named `dockhand/<first port>-<short changeset
    ID>`**, e.g. `dockhand/jq-4k2p`. Review 3.9, reopened. Named once at
    creation and never renamed, since a PR's head must not move; no verb,
    version, port set, or title in it, since all of those change; the ID
    is the changeset's, so the name identifies it. `--branch-name` sets
    one at creation; adopted branches keep theirs; existing
    `dockhand/bump/…` and `dockhand/revbump/…` branches are untouched. No
    `changesets/` level: dockhand makes one kind of branch, the name is
    public in a PR's head, and a later kind can take its own subfolder
    beside these.
38. **Two Tart findings settled after testing.** Disk growth: the
    host-side recovery-partition edit is a flagged, approved exception to
    decision 32, because the only automated route, the one Tart's FAQ
    points to (Cirrus's Packer plugin), does the same with go-diskfs on
    `disk.img`; it runs only during setup, on dockhand's own freshly
    cloned stopped `-next` VM, only when `tart get` says `DiskFormat`
    raw, on every image so the 100 GB disk is usable, with the guest
    agent's resize logged. File transfer: SSH/SFTP to the address `tart
    ip` gives, for logs, archives, and staging, in both directions, using
    `golang.org/x/crypto/ssh` (already vendored, used by setup) and
    `github.com/pkg/sftp` (maintained, BSD-2, v1.13.11 of 2026-07, adds
    only `github.com/kr/fs`); `tart exec` for commands and small outputs;
    every transfer checked against a size and sha256 computed on the
    other side, since `exec` can truncate while reporting success.
    Decision 28's archive transfer uses this channel. The `exec` failure
    on macOS 12–15 guests went to the session that filed #1344 and #1345,
    to reproduce, attribute, and draft for review. It was filed on
    2026-09-24 as openai/tart#1347 (guest output lost), #1346 (the control
    socket stops accepting connections after a failure, still on Tart main
    at `4e58a2a`), and #1348 (`tart exec -i` holds all of stdin in memory).
    The loss is below Tart and the agent: a raw `AF_VSOCK` sender in a
    Sonoma guest, with no agent or gRPC, lost 64 KiB blocks after about
    256 KiB, while the same host code was clean with a Tahoe guest. On
    Monterey a command ending `exit 7` returned 7 while whole frames of its
    output were lost, so no exit status establishes that output arrived.
    Therefore everything dockhand reads back as data from a guest moves to
    SSH/SFTP, `result.json` included, not only logs; `tart exec` is kept for
    commands whose output dockhand does not rely on, and any output it does
    rely on is checked against a size and hash. SSH/SFTP runs over the
    guest's virtio-net interface, not vsock; the implementation's tests
    include large transfers out of macOS 12–15 guests.
39. **MacPorts Base gets version-specific shims, designed in a dedicated
    session.** Base is carved out of decision 32's official-interfaces
    policy: no public interface gives the evaluator's fidelity, and Base's
    own developer drives it through `port-tclsh` and internals kept
    working with version-conditional code (mpbb `02cfeb1`). Dockhand
    writes against the current release, 2.12.6, with explicit shims and
    guards for what Base master already shows: Pextlib loaded only inside
    `mportinit`, fetch code loaded lazily, compiler checks in the parent
    interpreter, helpers turned into parent aliases, Tcl 9. The same idea
    as the roadmap's "add version-specific adapters only for demonstrated
    differences". The survey's classification, mpbb's patterns, and the
    proposed guards are the session's starting material; the startup
    check's `vercmp` defect is fixed regardless, first in the queue.
40. **Preparation may add and delete files inside the port directories the
    changeset edits**, `files/` included, never `_resources`. `EditTree`'s
    refusal of a file absent from the base (`workspace-design.md:349`) is
    lifted by extending the overlay to carry new entries and drop old
    ones; fidelity checks apply to the result. A later preparer can drop a
    patch that applies cleanly in reverse against the new source, so is
    provably merged upstream, with its `patchfiles` entry.
41. **No direct use of Apple's Virtualization framework now.** Considered
    on 2026-09-24: it is Apple's public API, and Go bindings exist
    (`github.com/Code-Hex/vz`, MIT, used by `vfkit`; last release 2025-08).
    Against it: dockhand's images are Cirrus's Tart-format OCI images, so
    reading them directly would couple to Tart internals, and making images
    ourselves means restoring IPSWs and driving Setup Assistant as Cirrus's
    Packer templates do; the `com.apple.security.virtualization`
    entitlement and code signing in dockhand's MacPorts port; cgo in a
    pure-Go tool with a Linux build; and owning cloning, MAC and IP
    handling, disk growth, image pulls and caching, and the guest channel
    through every macOS release. Its main apparent benefit, a vsock
    channel of our own, fails the same way `tart exec` does on macOS 12–15
    guests (#1347). The problems found are handled: ASIF declined (32),
    deletes confirmed through `list` (31), files over SSH/SFTP (38), and
    Tart internals never touched. Instead: fixes offered upstream to Tart,
    which merges outside PRs, and the provider seam kept (`verify.Provider`
    already has Tart and GitHub), so a framework-backed provider could sit
    beside them later. Reopened if Tart becomes unmaintained, if a blocking
    bug stays unfixed across releases, or if dockhand needs something Tart
    cannot provide.
42. **Dockhand gets its own Tart home, and SSH is verification's only
    guest channel.** A running ASIF VM of the person's in a shared Tart
    home would stall dockhand's `tart list` (#1344); a Tart home of its own
    (`TART_HOME`, an official setting) keeps the person's VMs out of
    dockhand's view entirely. Apple's two-VM limit stays machine-wide, so
    the person's running VMs still count against capacity. Existing images
    move with Tart's official `export` and `import` rather than being
    provisioned again; the Tahoe images are rebuilt anyway for the pinned
    tools generation (decision 13). Verification talks to its guest only
    over SSH: launching the build, checking status, reading results and
    logs, and staging, so `tart exec`'s data loss (#1347), control-socket
    wedge (#1346), and stdin buffering (#1348) cannot reach it; setup
    already bootstraps over SSH. Proven before it is built: a teammate is
    testing SSH/SFTP (OpenSSH and the Go libraries) on Monterey, Sonoma,
    and Tahoe guests with 1 and 4 GiB transfers both ways, small-command
    round trips, transfers during a build, and connection stability.
    Confidence in the other Tart workarounds, assessed 2026-09-24: high
    for ASIF declined (#1344), deletes confirmed through `list` (#1345),
    and name checks before `clone` and `rename`; verdicts are safe today
    because `result.json` (about 2.5 KB) fails to parse rather than
    misleads when truncated and the failure summary is computed in the
    guest, but host-side build logs from macOS 12–15 guests can be
    silently incomplete until the SSH channel lands.
43. **Guests are reached through Apple's `/usr/bin/ssh`, with a dockhand
    key pair.** macOS Local Network privacy blocked a Go binary's direct
    dial to a guest depending on which app launched it, while Apple's own
    binaries are not subject to it; `/usr/bin/ssh` is also macOS's
    official, documented tool (decision 32). Commands run over one
    multiplexed connection per guest (`ControlMaster`; about 3 ms a
    command); files go through `pkg/sftp` over `/usr/bin/ssh -s … sftp`
    or `ssh … cat`, the fastest mode tested; every transfer is checked by
    size and sha256, which also covers `pkg/sftp`'s concurrent writes
    leaving holes on error. The client sends keepalives (sshd sends none)
    and stays within ten sessions per connection. Host keys are baked
    into each Cirrus image and shared by every clone, so setup records
    each image's host keys and verification checks the guest against that
    record, not a `known_hosts` keyed by IP. Setup installs a dockhand key
    pair, kept under `~/.dockhand`, in place of the default `admin`
    password, and setup itself moves to this transport, which fixes its
    current `x/crypto` dial that works only when the launching app has
    Local Network permission.
44. **The changeset model, tightened after an independent review.** The
    [review](2026-09-24-changeset-roadmap-review.md) agreed the changeset
    should be dockhand's primary unit of work and found five gaps, each
    checked and adopted (`record.Attempt` carries one `TargetID` and one
    `BuildSpec` with one `Target`, `record/attempt.go:118–123`, `:175–176`;
    `Artifact.Digest` and `BuildSpec.Inputs` exist).
    - **Vocabulary.** Changeset (identity, branch and PR association,
      title, lifecycle); revision (immutable candidate source and base,
      the source of the changed scope); verification plan (frozen
      coverage, exclusions, prerequisites, and environment choices for a
      revision); guest execution (one admitted run per changeset and
      platform, owning the VM); target result (what happened to one target
      and configuration with particular inputs). A port stays the editing
      target and the command shortcut; changeset status explains itself
      through its target results.
    - **Revision-only is decided from the source.** A target is
      revision-only only when the Portfile diff touches nothing but
      `revision` declarations, shared code included, and no `files/`
      change; metadata equality supports the check but never proves it
      (deferred code such as a `post-destroot` body is not in the metadata).
      Dockhand's own revision bumps prove it from their edits; adopted or
      amended code is checked the same way; unknown is substantive; shared
      declarations affecting siblings get an explicit rule. Amends 21 and
      22.
    - **The plan separates** the changed scope; CI eligibility per
      platform with the reason for every exclusion; the selected coverage
      (`--only`, `--also`); and the prerequisites that coverage needs.
      Changed prerequisites are included automatically and shown, so `--only
      appB` still builds the changed `libA` it depends on, and `appB` is
      blocked if `libA` fails; an old binary is never substituted. A target
      whose evaluation fails leaves the plan unresolved; it is never
      dropped as ineligible. Eligibility and prerequisites are
      platform-specific; the selection is frozen at intake and the plan
      bound to its revision and environment. Publication requires the
      selected substantive targets to pass; status and the PR list what was
      selected and passed apart from what was not built, never a blanket
      "verified". Amends 22 and 23.
    - **Reuse records observations and artifact identities.** The record
      holds the queries evaluation made and their answers, negative ones
      included (a file absent, a name not found, a directory's listing, a
      symlink's resolution), not only the files read; where the record is
      incomplete, the host-only reuse path invalidates conservatively. Each
      build records its input identity and the digest of every archive it
      consumed, carried through dependencies, so an amended `libA` with
      unchanged coordinates cannot be reused through, and a request's
      identity is kept apart from its output's digest. Evidence validity
      and archive availability are separate facts: an archive is ready for
      dependents only once its transfer and checksum are durably recorded,
      and cleanup respects every live reference to it. Amends 28 and 36.
    - **The guest execution has one owner** with durable per-target
      checkpoints beneath it: a target that failed, one interrupted
      mid-build, and one blocked or not started are distinct; retry
      consumes only complete applicable results with their archives
      available; messages from a superseded guest are rejected. Settled
      before any per-target retry or reuse is built.
    - **A baseline never unblocks publication by itself.** It is reported
      as evidence, "also failed on the base revision in the same package
      and phase", linking both results, and the person still acknowledges.
      Supersedes decision 20's rule that an already-broken dependent does
      not block; publication rests on acknowledgement (decision 21).
    - **Order.** A consolidated changeset design document, the current
      contract in this vocabulary rather than a trail of amendments, is
      written before any schema change; then a narrow complete path
      (adopt a two-directory branch, freeze its plan, verify in one guest
      with per-target checkpoints, publish with its coverage described
      accurately), with whole-tree invalidation at first; reuse and
      archives only after identities and checkpoints work; conveniences
      after. Acceptance cases: a library and two consumers with `--only`
      selecting one consumer, which still receives the candidate library;
      an amend changing the library's code but not its coordinates, so no
      consumer reuses; a previously absent optional file added, which
      invalidates reuse with no guest running; a revision bump that also
      edits a build hook, which is substantive; a guest lost during the
      third target after two completed, whose results survive; a one-port
      contribution migrated without its evidence covering newly derived
      siblings.

## MacPorts in a controlled room

Keep MacPorts Base as the interpreter, since MacPorts is the authority on
what a Portfile means. Replace how dockhand controls what that interpreter
asks the host. Today four mechanisms cover parts of it and none prevents
anything: platform variables overridden after init; execution traces on
`exec`, `file`, `open`, `source`, and `glob` that record host access only
when a Portfile frame is on the stack; the Linux model's replacement
`file`; and the static Go grammar for pre-fetch hooks. Top-level Portfile
and PortGroup code runs `exec` on the host for real
(`cli-design.md:149`: "this is not a sandbox for untrusted Portfiles").

Rejected: writing a Tcl evaluator of our own or running Base's scripts on
one (Base leans on C extensions, pextlib, registry2, fastload, and Tcl 8.6
semantics; it would be a second MacPorts that drifts); symbolic evaluation
yielding every platform from one run (Tcl builds code from strings).

The shape:

1. **Inventory.** Across the survey, native and modeled, callbacks
   included, record every command that reaches outside the interpreter:
   Tcl's I/O, pextlib's commands, `registry::`, Base's fetch. What
   Portfiles actually ask the host, by frequency. Also what `mportinit`
   asks in the parent before any worker exists.
2. **One oracle.** `interp hide` those commands in the worker and alias
   them to one dispatcher that knows the context and answers from the
   captured tree, from the host (native only, reads and table programs),
   from the platform model (modeled), or refuses (writes, network,
   unlisted programs), recording every answer with its source. A refusal
   makes the context inconclusive, never guessed. Replaces the traces,
   the Portfile-frame condition, and the replacement `file`, and is the
   effect layer.
3. **Platform models as data.** Per release: Darwin number, product
   version, toolchain, SDK paths and versions, the answers a Mac of that
   release gives. Facts are added as evidence arrives; pyqt5 first.
4. **Hooks stay judged statically**, with the read-only program table in
   the oracle's policy so the guard and the evaluator share one list.

What the survey says it is worth (2026-09-21 baseline, 94.1% of 41,730
ports input-found): "host state read during evaluation" is the largest
remaining bucket at 674 ports, of which 366 are the six frozen qt5x
Portfiles judged not worth modeling, 33 php, about 275 others led by
pyqt5. The case is safety and one mechanism in place of four as much as
coverage.

### The inventory, 2026-09-23

A scratch build (`/tmp/dockhand-inventory`, instrumentation in
`evaluator.tcl` behind `DOCKHAND_HOST_INVENTORY`, not for check-in) traced,
in every port worker from `PortSystem` on, each Tcl, Pextlib, and
parent-alias command that can reach the host, and every read of `::env`,
with the port-tree frame that caused it, whether a registered callback was
running, the context, and the class of path touched. `assess --all` over
the committed tree at `227a7cfe1ad`: 41,771 ports (39,491 input-found,
1,230 unsupported, 1,050 unknown), 62.6 minutes against 48.8 untraced,
4.35 million distinct records. Output and analysis under
`~/.dockhand/surveys/2026-09-23-host-inventory/`. `mportinit`'s own
questions in the parent were read from Base 2.12.6's source instead.

- **Evaluation is read-only in practice.** No port code or Base code
  wrote a file, opened one for writing, or touched the network during
  evaluation anywhere in the tree. Processes ran for about 210 ports and
  some 20 distinct programs: the Java PortGroup's `java_home` (183 ports,
  from its callback), `xcrun --show-sdk-path` (Base and a few Portfiles),
  `sysctl`, `uname`, `machine`, `git -C` on the captured tree, `rustc`,
  `clang --print-search-dirs`, and the prefix's `perl` and `ruby` for
  their configuration. Enforcing the effect layer would cost almost
  nothing today: an allowlist of read-only programs and refuse the rest.
  Not seen by the trace: work before `PortSystem`, I/O inside C
  extensions (the registry's SQLite), and `mportinit`, which runs `sw_vers`,
  `sysctl`, `dscl`, `id`, reads the environment and MacPorts'
  configuration, lazily `xcodebuild` and `xcode-select`, and can create
  `portdbpath` and set its hidden flag.
- **Modeled contexts take the host's toolchain by design.**
  `macports.PlatformVariables` overrides the OS, architecture, versions,
  deployment target, universal archs, and C++ library, and nothing about
  the toolchain; its neighbor `ModelVariables` says "a Mac never needs
  this: its own tools answer for every platform it models". So a Darwin 22
  context is evaluated with this host's `xcodeversion`, `developer_dir`,
  compilers (`get_tool_path`, `get_compiler_version`, `/usr/bin/clang`,
  the tools' `make`, `libxcselect`), and `MacOSX26.sdk`. Base asks these
  in modeled contexts for about 22,000 ports. This contradicts
  `bump-planner.md:22`, "host-dependent execution that defeats the model
  must remain a reported gap". The exposure for checksums looks small: a
  rough scan finds fetch inputs branching on the Xcode version only in the
  six frozen qt5x Portfiles, already unknown. For compiler dependencies
  and the dependency closure it is wider.
- **Port-code host reads the observation does not flag in modeled
  contexts:** the R PortGroup's `xcodeversion` (3,071 ports), compiler
  lookups a Portfile triggers (about 1,000), `qt5_version_info`'s
  `registry_active` (713, of which 415 end unknown), the Java callback's
  `exec`, `JAVA_HOME`, and JavaVM directory (148; the callback gap,
  confirmed), boost's and openssl's compiler lookups (about 130 each),
  `env(PATH)` (qt4 134, Portfiles 99), `findBinary` (105), `sysctl` (39).
  Registry, environment, and alias-command reads are not traced by the
  observation at all.
- **Native contexts read the person's installation.** `registry_active`,
  prefix files (qt4, python, php's ini, gnustep, emacs), prefix compilers
  (`clang-mp-*`), and `$PATH` answer from what this Mac has installed. A
  Tart guest or a GitHub runner is a fresh MacPorts with nothing
  installed, so the native evaluation is not the builder's either.

What the oracle would answer, by weight: the toolchain of each release
(Xcode and tools versions, developer directory, which compilers exist and
their versions, SDK paths and versions) covers the bulk; the installation
(registry, prefix files and programs), where the builder's truth is a
fresh prefix; the environment (`PATH`, `JAVA_HOME`) as MacPorts' build
environment; hardware (`sysctl` keys, `uname -m`); and about twenty
read-only programs.

### When a Portfile sees its dependencies

Within one `port` command (Base 2.12.6, `mportexec` in `macports.tcl`),
`mportopen` evaluates the Portfile's top level and registered callbacks,
`mportdepends` then installs the dependencies, and `eval_targets` runs the
phases and hooks in the same interpreter with no re-evaluation. So
`distfiles`, `checksums`, `depends_*`, and compiler selection are set
before dependencies exist; phases and hooks run after (the Java pre-fetch
hook rechecks "in case java became available e.g. openjdk installed as a
dependency").

MacPorts' CI (`macports-ports/.github/workflows/main.yml` with `mpbb`)
runs `port lint`, then `mpbb install-dependencies` (the dependencies, and
only those, installed and active), then `mpbb install-port`: `port -d
fetch`, `port -d checksum`, `port -dkn install --unrequested`, each a new
command. The target is evaluated, and its fetch plan computed, with its
dependencies active.

Dockhand's Tart guest (`internal/verify/tart/guest.tcl:262-276`) runs
`lint`, `-d build`, `test`, `install` on an image checked to have nothing
installed, so its `build` evaluates the target with an empty prefix and
installs dependencies partway through. A Portfile whose top level reads
installed state (`qt5_version_info`'s `registry_active`, python's include
directories, `perl -V:vendorlib`, `clang-mp-*` for compiler selection) is
evaluated differently from MacPorts CI. The GitHub provider runs MacPorts'
own workflow and does not differ.

The installation to model is therefore the target's dependency closure
active and nothing else, from two passes as MacPorts does: evaluate for
dependencies, then evaluate with installation questions answered from the
closure. Registry questions can be answered from the index's closure
(default variants, an estimate as for dependents); prefix file and program
questions only where a path maps to a port, since the index does not list
a port's files.

### Toolchain facts from MacPorts' buildbots

`build.macports.org` (Buildbot 0.8, `/json` API) runs builders for macOS
10.6 i386 and x86_64 through 27 arm64. Each `install-port` step's `stdio`
log carries the `port -d` header: macOS version and Darwin, MacPorts
version, `Xcode <v>, CLT <v>`, SDK, `MACOSX_DEPLOYMENT_TARGET`, then the
preferred compiler list, the compiler chosen, `CC`, and sometimes the
AppleClang version. Harvested from the latest build on every builder on
2026-09-23 (`~/.dockhand/surveys/2026-09-24-toolchain-facts/buildbot_facts.sh`):

| builder | Darwin | Xcode | CLT | SDK |
|---|---|---|---|---|
| 10.6 i386, x86_64 | 10 | 3.2.6 | none | 10.6 |
| 10.7 | 11 | 4.6.3 | none | 10.7 |
| 10.8 | 12 | 5.1.1 | none | 10.8 |
| 10.9 | 13 | 6.2 | 6.2 | 10.9 |
| 10.10 | 14 | 7.2.1 | 7.2 | 10.10 |
| 10.11 | 15 | 8.2.1 | 8.2 | 10.11 |
| 10.12 | 16 | 9.2 | 9.2 | 10.12 |
| 10.13 | 17 | 9.4.1 | 9.4.1 | 10.13 |
| 10.14 | 18 | 10.3 | 10.3 | 10.14 |
| 10.15 | 19 | 11.7 | none | 10.15 |
| 11 arm64, x86_64 | 20 | 13.0 | 13.0 | 11 |
| 12 arm64 / x86_64 | 21 | 14.0.1 / 13.4.1 | 14.2 / 13.4 | 12 |
| 13 arm64, x86_64 | 22 | 14.3.1 | 14.3.1 | 13 |
| 14 arm64 / x86_64 | 23 | 15.4 | 16.2 / 15.3 | 14 |
| 15 arm64 / x86_64 | 24 | 16.4 | 16.4 / 26.0 | 15 |
| 26 arm64, x86_64 | 25 | 26.6 | 26.6 | 26 |
| 27 arm64 | 27 | 27.0 | 27.0 | 27 |

The 27 builder confirms Golden Gate is Darwin 27. Tart on Apple silicon
boots arm64 macOS only, so x86_64 contexts, which preparation models
routinely, can only come from here. Builders differ: the buildbots carry
full Xcode, usually with the tools; dockhand's base images the tools only;
GitHub's runners their own Xcode. Darwin 8 and 9 have no builder.

Decided (Q13, decision 12 above).

### Toolchain facts from dockhand's Tart images

All ten images probed on 2026-09-23 (`~/.dockhand/surveys/2026-09-24-toolchain-facts/tart/`:
`probe.tcl` asks through Base's own procedures and a throwaway Portfile's
worker; one JSON per image; `summary.md`). All arm64:

| Darwin | profile | Xcode | CLT | SDK Base uses | compiler, CC | Base's clang |
|---|---|---|---|---|---|---|
| 21 | CLT / Xcode | none / 14.2 | 14.2 | 12.3 | clang, /usr/bin/clang | 1400.0.29.202 |
| 22 | CLT / Xcode | none / 15.2 | 14.3.1 | 13.3 | clang | 1403.0.22.14.1 |
| 23 | CLT / Xcode | none / 16.2 | 16.2 | 14.5 | clang | 1600.0.26.6 |
| 24 | CLT / Xcode | none / 26.3 | 16.4 | 15.5 | clang | 1700.0.13.5 |
| 25 | CLT / Xcode | none / 26.6 | **27.0** | 26.5 | clang | 2100.3.34.2 |

What it means for the table and for setup:

- **Setup installs whatever Command Line Tools sort last.**
  `internal/macos/install.go:41` takes `softwareupdate --list | sort |
  tail -1`, so both Tahoe images carry the macOS 27 tools (clang
  2100.3.34.2, SDKs 26.5 and 27.0) while MacPorts' 26 builders run 26.6.
  Dockhand's Tahoe verifications build with a newer toolchain than
  MacPorts does. Monterey through Sequoia match the buildbots' tools
  exactly. `macos.CurrentToolchain` (Xcode 26.3, clang 1700.6.4.2)
  matches no Tahoe image; it is the Sequoia Xcode image's.
- **The Xcode profile does not change the compiler.** On every Xcode image
  `use_xcode` is 0 (Darwin 20 and later with the tools' `make` present),
  so the developer directory, the SDK, and the compiler are the tools'.
  The table keys the compiler to the Command Line Tools in both profiles;
  Xcode matters only to ports that say `use_xcode yes`.
- **Base uses `MacOSX<major>.sdk`, not xcrun's default** (12.3 on
  Monterey where xcrun says 13.1). The "SDK N" line in Base's header,
  and so in the buildbot logs, is `macosx_sdk_version`, the OS major,
  not the SDK used.
- **`xcodebuildcmd` is `/usr/bin/xcodebuild` with no Xcode installed**;
  not a signal of Xcode.
- **`compiler.fallback` is not a platform fact.** It comes from the
  tree's `clang_compilers.tcl`, filtered by the port's
  `compiler.cxx_standard`; the table holds what feeds it, not it.
- `/usr/lib/libxcselect.dylib` is absent on every image; Base falls back
  to `os.major >= 20`.
- The Xcode images carry newer Xcodes than the buildbots on four of five
  releases; the guests' macOS point releases trail the builders' on
  Ventura, Sonoma, and Sequoia.

## Proposed, waiting on a decision

- **Q10. Order of work**: delegated. The implementation order is Claude's
  to set and re-settle as decisions land (2026-09-23).
- **Q14.** Closed by decision 22.
- **Q16.** Closed by decisions 19 and 22: a port may be in several
  changesets; a dependent's bump in one names the others in the plan.
  Never folded in.

## Found along the way

- **Golden Gate is Darwin 27, not 26.** A Golden Gate guest reports
  kernel `Darwin 27.0.0` (build 26A428); Tahoe is 25. Apple skipped 26.
  `internal/macos/release.go:45` maps Golden Gate to Darwin 26, so
  `--os golden-gate` would accept a Darwin 26 platform the guest never
  reports, a Golden Gate host would find no release, and modeled contexts
  would model a Darwin that does not exist. Fix the entry and anything
  that assumes consecutive Darwin numbers.
- **Tart runs a guest newer than its host.** Cirrus Labs'
  `macos-golden-gate-vanilla:27.0` booted headless on a Tahoe 26.6.2 host
  and answered over SSH. Apple's restriction is on installing a newer
  guest from an IPSW, which dockhand never does.
- **`dockhand setup --os golden-gate` provisions**, with the Darwin 27
  fix applied in a scratch build: guest agent, Command Line Tools for
  Xcode 27.0, MacPorts 2.12.6 from `MacPorts-2.12.6-27-GoldenGate.pkg`.
- **`tart list` fails while an ASIF-disk VM runs.** Golden Gate images
  use Apple's sparse image format (`shdw` magic; Tahoe's are raw). Tart
  2.37.0 runs `image info --plist` on every VM's disk to fill `list`, and
  that fails with "Resource temporarily unavailable" on a running ASIF
  disk, failing the whole listing. Setup could not stop its own guest,
  and its failed-guest cleanup, which also stops through the listing,
  left the VM running. Every dockhand Tart operation lists VMs
  (`host.Machine.LocalVM`, `host.Machine.Running`, provisioning), so any
  running ASIF VM, dockhand's or the person's, breaks all of them.
  Explored separately; what it found:
  - `list` (every form but `--source oci`) and `get <that VM>` fail for
    as long as the VM runs; retrying never helps. `get` of another VM,
    `ip`, `set`, and `stop` work. Raw disks never fail. The
    Virtualization.framework process holds an exclusive flock on
    `disk.img`; for ASIF, Tart's `diskSizeBytes()` shells out to
    `diskutil image info --plist`, which hits the lock, and one failing
    entry aborts the whole listing with no partial output. Tart HEAD
    (now `openai/tart`, 2026-09-21) is unchanged; no upstream issue.
  - A second Tart bug, raw and ASIF alike: `tart delete <running VM>`
    exits 2 saying the VM "does not exist", an error-code collision
    between `VMIsRunning` and `NSFileNoSuchFileError`. Dockhand must never
    read that message as success.
  - In dockhand, `Client.Images` (`internal/tart/images.go:15`) is the only
    `tart list`, and everything that asks whether a VM exists or runs
    goes through it. Setup's `Stop` fails before it ever calls `tart stop`;
    `Machine.Delete` fails before `tart delete`; `cleanup`
    (`provision.go:343`) discards both errors; `StartForeground` keeps no
    handle on the `tart run` child. Hence the VM left running. In the
    verify provider, `Capabilities` calling `Running` would block
    submissions, observations, and releases for every OS while any ASIF VM
    runs, and `Inspect` could never see a Golden Gate run finish.
  - Recommended: answer per-VM questions from the VM directory the way
    Tart does, existence from `vms/<name>/config.json` and running from
    `fcntl(F_GETLK)` on it (Tart's `PIDLock`), behind one helper in
    `internal/tart`; prototyped in Go and correct while `tart list`
    failed. Setup's stop always runs `tart stop`, treats "not running" as
    success, and falls back to SIGINT then SIGKILL on the kept child;
    cleanup reports its errors and deletes even when stop failed; delete
    is confirmed by the directory being gone. Upstream fixes (size from
    the ASIF header or unknown; the error-code collision) not filed.
  - Open: coupling to Tart's lock file, or `tart list` first with the
    lock check as fallback; whether setup's `tart run` gets its own
    process group.
- **A named subport with an obsolete follower is refused after acceptance.**
  `bump kubectl-1.37` (2026-09-23) previews a correct diff, moving the
  obsolete `kubectl` stub's `version 1.37.0` with `patchNumber`, but the
  job stops with "workflow: unapproved shared-release scope".
  `fidelity.ReleaseScope` lets an obsolete follower move without
  shared-release authorization (`fidelity.go:297`), and the editor records
  a scope that includes followers (`portedit/version.go:73`); the
  workflow refuses any recorded scope without `SharedRelease`
  (`workflow/preparation_run.go:188`), which binding sets only for stubs
  and main ports (`preparation_bind.go:126–141`). The preview never runs
  the workflow's check. Workaround: `--shared-release`. Queued with the
  defects: record why each member is in the scope (`Follower` on
  `record.ReleaseMember`, omitempty, no migration), one predicate for
  "needs shared-release authorization" called by the editor, the
  workflow, and the preview alike, and a workflow test that bumps a named
  subport with a terraform-shaped stub through the real cycle (the
  `terraform` fixture in `workflow/source_test.go:244` covers selection
  by the main name only).
- **Dockhand will refuse to start on the next MacPorts Base release.**
  `check_startup` checks `::vercmp` before `mportinit`
  (`eval/compatibility.tcl:12`). In 2.12.6 Pextlib, which provides it,
  arrives through packages `macports.tcl` loads at its top; on Base
  master (e545ebe8c, "Defer loading of some packages") Pextlib is loaded
  only inside `mportinit` (`macports.tcl:1145`). Queued first among the
  defects: check after `mportinit`, or `package require Pextlib` first.
- **How dockhand's Tcl sits against Base (survey, 2026-09-23).** Its core
  pattern (`mportinit`, `mportopen file://…` with subport and variants,
  `mportinfo`, `ditem_key … workername`, `$worker eval`, `mportclose`)
  is how @jmroot's gists and mpbb drive Base; `macports::override_vars`
  is established too (mpbb's `mirror-multi.tcl`, Base's `portindex -p`,
  @jmroot's per-platform lists in mpbb's `index_vars/`). Dockhand-only:
  execution traces on internal procedures, the Linux model's `::file`
  rename, target and hook records, copies of Base's livecheck and fetch
  logic, writes to `::macports::sources*`, the fetch guard's definition
  walk. Mostly stable since 2.8; Base master (229 commits past 2.12.6)
  breaks four: the startup check above; fetch internals loaded lazily
  (`portfetch::checkfiles` and `fetch_main` absent after `mportopen`,
  which would disable archive preparation for every port); compiler
  checks moved to the parent interpreter, out of the worker shim's
  reach; many helpers turned into aliases to parent `portlib::*`, so the
  definition walk refuses more hooks (safe, narrower). Master also moves
  to Tcl 9. Scratch and per-tag Base copies: `/tmp/tclsh-survey/`.
- **Tart's official interfaces, tested (2026-09-24, Tart 2.37.0,
  `/tmp/dockhand-tart-tests/`).** Sufficient: half-finished clones (Tart
  builds a clone in a locked `~/.tart/tmp/<UUID>` and moves it into
  `vms/` in one step; a killed clone leaves nothing or a tmp directory
  Tart's next command removes; a fresh clone always succeeded), so
  `LocalVM`'s directory check can go; pull and clone by digest (registry
  API digest, `tart clone …@sha256:…` from the cache). Pinned shapes:
  `list` entries `Accessed`, `Disk`, `Name`, `Running`, `Size` (int),
  `Source`, `State`; `get` `CPU`, `Disk`, `DiskFormat`, `Display`,
  `Memory`, `OS`, `Running`, `Size` (string), `State`, no `Name`, key
  order varying; `stop` of a stopped VM exits 2 "is not running";
  `delete` of a running VM exits 2 "does not exist" (#1345) and leaves it.
  Guards needed: `tart clone` onto an existing stopped VM's name silently
  replaces it, and `tart rename` works on a running VM, so dockhand
  confirms a target name is absent through `tart list` first.
- **The recovery partition cannot be removed in the guest.** SIP refuses
  in a normal boot ("an APFS Recovery Physical Store… csrutil disable
  from the Recovery OS"), and every variant was refused. The guest
  agent's `--resize-disk`, which dockhand runs (`--run-daemon`), grows
  the container only with nothing after it (its README: "with recovery
  partition removed") and fails silently (-69519) since dockhand's plist
  gives it no log path. The only automated route, the one Tart's FAQ
  points to, is Cirrus's Packer plugin editing `disk.img` on the host with
  go-diskfs, as dockhand does. Plain images are affected too: 100 GB
  disks with a 44 GB container, about 50 GB unusable.
- **`tart exec` cannot reliably carry data out of macOS 12–15 guests.**
  Guest to host works on Tahoe (about 325 MiB/s) but fails on Sonoma
  within 1 MiB ("Transport became inactive"), on Ventura and Sequoia at
  16 MiB or less, and on Monterey returned success with truncated data in
  7 of 8 reads; failures sometimes wedged the control socket until a VM
  restart, and one read hung over 6 minutes; only per-call chunks of 192
  KiB or less survived, at 1–5 MiB/s. Host to guest works (about 255
  MiB/s) but `tart exec` grows to about 2.2 times the payload in memory.
  A live defect: dockhand reads build logs by `cat` over exec
  (`verify/tart/native.go:115`), so logs on Monterey–Sequoia guests can be
  silently truncated or leave the agent wedged. Queued with the defects.
  VirtioFS (`tart run --dir`) moved 1 GiB each way in about 1 s but stays
  excluded (#1308, #1330).
- **MacPorts Base, the review and the baseline (2026-09-24).** An
  independent review
  ([2026-09-24-macports-base-independent-review.md](2026-09-24-macports-base-independent-review.md))
  was checked against the code and Base master and accepted almost
  whole: environments as process invariants (master's `portlib` caches
  files, SDK roots, compilers, and mirror URLs keyed by mirror file;
  `override_vars` removes traces); `PORTSRC` overlays rather than isolates
  configuration (`macports.tcl:1233`); refusals must survive `catch`
  through a ledger; semantic records instead of Base's representation
  (Go still compares `portfetch::fetch_main`, `fetchguard/grammar.go:26`,
  and strips Base's hook prefix, `:413`); verified descriptions of aliased
  helpers rather than a generic walk into parent bodies (definitions are
  keyed by spelling, `effect.go:21`); failure proportional to the missing
  capability; the preview identified by commit, never by 2.12.99. An
  executable baseline against `/opt/macports-master` then showed four
  small patches make the whole suite pass on master and 659 real
  Portfiles give identical fetch plans and guard verdicts; that master's
  parent-side compiler and SDK probes make 11 host-reading ports look
  host-independent without failing any test; that `PORTSRC`, an empty
  `HOME`, and a private `portdbpath` isolate the host's own 2.12.6 as well
  as a private install; and that a Base installed at one prefix can model
  `/opt/local`. The prospective design was rewritten around both
  ([macports-base-design-prospective.md](../macports-base-design-prospective.md)).
  The review's follow-up approved the revision after checking the saved
  baseline data, with two conditions adopted into the design: Base
  persists host facts under `${portdbpath}/cache` (`macports.tcl:642`,
  `:662`; verified), so the private `portdbpath` is disposable per process
  from an empty template rather than persistent; and step 1's startup fix
  ships with a version gate, since today only the startup check's
  accidental failure stops master from preparing with the eleven
  host-access false negatives. Also adopted: the bootstrap order
  (`macports::version` is available before `mportinit` on both Bases,
  verified), per-path capabilities, runtime identity including the build,
  and isolation claims qualified until the host-changing tests pass.
- **New defects from the baseline, queued:** the livecheck copy relies on
  Tcl 8.6's `glob` error, gone in Tcl 9 (Base's own guard at
  `portlivecheck_run.tcl:101` has the same problem upstream); the RPC
  layer's `encoding convertfrom` outside a `catch` in `loop.tcl` lets a
  non-UTF-8 argument or an unencodable reply end a Tcl 9 session; some
  tests take `port-tclsh` and `portindex` from PATH rather than the
  opt-in variable, and on this machine PATH starts with
  `/opt/macports-test/bin`, so they silently test that install. Also:
  the local master build compiled in `/opt/macports-test/bin` tool
  paths; rebuild with a clean PATH before relying on its builds.
- **SSH/SFTP to Tart guests, tested (2026-09-24; scripts and logs in
  `~/.dockhand/surveys/2026-09-24-ssh-channel-tests/`).** Reliable over
  virtio-net on Monterey 12.7.6, Sonoma 14.8.7, Sequoia 15.7.7, and Tahoe
  26.6.2: about 320 GiB moved with every sha256 and byte count matching,
  no truncation, hang, or dropped connection, including under a MacPorts
  build and across a 15-minute connection; in the same boots `tart exec`
  lost data on 12, 14, and 15. Throughput: OpenSSH `ssh … cat` 385–429
  MiB/s up and 367–411 down; Go session `cat` 238–306 up and 396–519
  down; `pkg/sftp` concurrent 226–303 up and 298–496 down (sequential
  71–133). Host memory flat at 13–21 MiB for 1 and 4 GiB. 500 sessions on
  one connection: p50 2.3–3.6 ms; a fresh connection with a command: p50
  47–75 ms. The blocker: macOS Local Network privacy. A Go binary's
  direct dial failed ("no route to host"; the unified log shows a local
  network block attributed to the responsible app), while Apple's
  `/usr/bin/ssh`, `nc`, and `curl` connected; whether dockhand's own dial
  works depends on which app launched it (this session's `dockhand
  setup` succeeded over SSH), so setup's current `x/crypto` dial is a
  latent defect. Guest facts: host keys are baked into each Cirrus image,
  shared by every clone and every user of it, and clones get new IPs;
  sshd sends no keepalives; MaxSessions is 10 per connection, an sftp
  subsystem counting; APFS containers are 41 GiB with 16–21 GiB free;
  `dockhand-base-sequoia` has no ports tree (harmless: verification
  stages its own). Settled as decision 43.
- **`verify --os` already exists (`b4a0553`, 2026-09-23, from a Linux
  session; design in `docs/build-platforms.md`; handoff in
  `activity/2026-09-23-handoff-to-macos.md`).** It builds a verification's
  targets on named releases in their own images, `--os available` naming
  every prepared release, with every named release required to pass; it
  refuses GitHub, `--image`, and `--dependents`, and reuse is not
  consulted with several builds. It agrees with decisions 4, 6, and 14 in
  shape. Where it differs, the decisions here supersede it (Herby,
  2026-09-24), and the code is aligned in step 1: `--os` adds releases to
  the host's rather than replacing it (decision 4); publication requires
  every requested release to pass rather than selecting the host's
  evidence (decision 14); and the refusal of an unnamed build on a release
  newer than `tart.DefaultDarwin` goes (decision 5). Its per-release Xcode need is read
  from the host evaluation, which decisions 7 and 9 answer with the facts
  table. Its hardware note, that a newer guest is generally unsupported,
  was corrected after the Golden Gate test. The handoff's Mac-only checks
  (the suite on macOS, the real Tart acceptance test, `verify --os` on
  real images, Linux modeling against a Mac, `git-devel` end to end, the
  crate alignment) join step 1.
- **Considered and deferred: `--with <subport>`** to sanction one
  sibling's version moving with the bump when it matches no recognized
  shape (literal owned by the sibling alone and equal to the target's old
  version, else refused). Not built until a port needs it; the shapes in
  use (terraform-style obsolete stubs, python stubs, shared main ports)
  are recognized, and `--shared-release` authorizes siblings wholesale.
- **PortGroup callbacks may escape host-access recording.** `record` in
  `observation.tcl` flags host access only with a `*/Portfile` frame on
  the stack; `port::register_callback` procedures run after the Portfile
  is sourced. The Java PortGroup's `java_set_env` is registered that way
  and runs `/usr/libexec/java_home` on every native evaluation of a Java
  port today. Confirmed by the inventory: in modeled contexts the Java
  callback's `exec`, `JAVA_HOME` read, and JavaVM directory check (148
  ports) went unflagged, and `::env` reads are not traced at all.


## Still to walk through

Settled: 3.1 by decisions 20–27, 3.2 by 28, 3.4 by 33, 3.6 by 29 and 30,
3.7, 3.8, and 3.9 by 35–37, the Tart interfaces by 32, 38, and the Base
direction by 39.

1. MacPorts Base shims (decision 39): the prospective design, revised
   after the independent review and the executable baseline, is in
   [macports-base-design-prospective.md](../macports-base-design-prospective.md),
   with five remaining decisions at its end.
2. Prose fixes: done 2026-09-24, uncommitted
   ([note](../activity/2026-09-24-stale-language.md)): phase language in
   `architecture.md`, `cli-design.md`, `components.md`, and `state.md`;
   retention in `architecture.md`; the `--dry-run --adopt` exception and
   the one-open-contribution wording in `resolution-design.md` (to the
   code as it is; decision 19 changes it when built); the driver wording
   of decision 33 in `principles.md` and `architecture.md`. Leftover: the
   CLI's `errNotImplemented`, referenced only by a test.
3. The optional-capability tests: one test per implementation asserting
   the optional interfaces it satisfies.
4. Settling this record into the roadmap: done 2026-09-24, uncommitted.
   The roadmap's Next is the queue in eleven steps, reordered after the
   changeset review (decision 44): defects; the guest channel; the
   changeset design document; host-independence foundations; the Base
   adapter boundary; a narrow complete changeset path; the oracle; reuse
   and archives; changeset conveniences; Tart on other releases, the
   configuration file, and cleanup; preparation coverage. Its other
   sections carry what the decisions changed.
