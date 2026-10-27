# Dockhand Design v3

Accepted direction, 2026-09-25, and built on `main` since. [The roadmap](roadmap.md) says what remains, [architecture](architecture.md) where each part lives in the code, and [usage](usage.md) how to use it.

## Sources and status

Design v3 combined Codex's terminal-workspace proposal (its spine: context from the worktree, authoring that doesn't commit, checking unfinished work, tidying by logical change, and explicit submission), the earlier terminal-experience draft (MacPorts' contribution specifics, `create` from a URL, `submit --passing`, and the needs-your-attention list), and the engine contract of the 2026-09-23 direction record, restated in v3's vocabulary. The contract decisions still in force are in [decisions](decisions.md), under their old numbers; the sources themselves are at tag `docs-archive-2026-10-25`.

Promises below that were built differently are corrected where they stand; ones declined since are struck or removed, and [decisions](decisions.md) says why.

This document is the consolidated contract that roadmap step 3 and decision 44 ask for before any schema change. It uses one noun throughout, the **branch**. The earlier documents' "changeset" means the same thing and is retired, so that there is no second word for one thing.

The audience is contributors at every level, served through progressive disclosure. The focus is one person on one Mac who maintains many ports.

## Contents

1. [The idea](#1-the-idea)
2. [Principles](#2-principles)
3. [The model](#3-the-model)
4. [Context and selection](#4-context-and-selection)
5. [Commands](#5-commands)
6. [Journeys](#6-journeys)
7. [Check](#7-check)
8. [Tidy, and what MacPorts asks of commits](#8-tidy-and-what-macports-asks-of-commits)
9. [Submit](#9-submit)
10. [Status and the attention list](#10-status-and-the-attention-list)
11. [Serve, the queue, and instance coordination](#11-serve-the-queue-and-instance-coordination)
12. [Output, scripting, and configuration](#12-output-scripting-and-configuration)
13. [Promises](#13-promises)
14. [What v3 changes in the direction record](#14-what-v3-changes-in-the-direction-record)
15. [Where v3 pushes back](#15-where-v3-pushes-back)
16. [From today's commands](#16-from-todays-commands)
17. [How to judge it before building it](#17-how-to-judge-it-before-building-it)

## 1. The idea

**Make a contribution, work on it freely, check it as often as useful, shape it for review when it's ready, and submit it.**

The unit of work is a **branch**: an ordinary Git branch of the ports tree, its base on `master`, and its relationship to at most one pull request. Its ports come from its diff. A branch can hold one new port, an update, a patch fix, a PortGroup change, or a coordinated change across many ports, such as a library update and the rebuilds of the ports that link it. The commits express what the author means for review. The file tree decides what is built.

```text
start → edit → check → tidy → submit
          ↑       │       │       │
          └───────┴───────┴───────┘
```

These are activities, not stages you must pass through in order. You can adopt a branch you already have, check before committing anything, keep a series of commits that's already good, submit a draft, or go back to editing after review.

Every command has a single job:

- Authoring commands (`update`, `checksums`, `revbump`, `create`, `edit`) change files.
- `check` builds and tests.
- `tidy` shapes the history.
- `submit` opens or updates the PR.

Shortcuts combine these explicitly, and each command means the same thing at every level of experience.

## 2. Principles

1. **The branch is the unit of work, and the worktree or `--branch` gives the context.** A port name is something to edit or filter by. It never silently chooses a branch.
2. **Authoring edits files; committing is deliberate.** It happens through Git, or through `tidy`. Dockhand helpers never choose which existing commit to amend.
3. **Checking unfinished work is normal.** A check captures the working files as a numbered snapshot, and its result belongs to that snapshot. Editing afterwards never turns an old result into a current pass.
4. **History follows logical changes.** Grouping by port is `tidy`'s suggestion, never a rule. MacPorts asks for commits minimized "ideally with one commit per logical change", and for follow-ups to be squashed.
5. **Coverage and evidence are named.** Results say which targets, on which provider and release, at which snapshot or commit. Nothing says just "verified".
6. **One driver at a time, and observers see everything.** `serve` drives the queue. Without it, a command runs its own request in the foreground and says so. Detaching from `serve`'s work is always safe.
7. **Publishing is always a person's decision.** Either the decision is about an exact revision (`submit`, or running `ship` or `bump`, which is the consent), or it's a standing one the person gave `serve` (`serve.ship_to = "submit"` in the config, or `serve --ship-to submit`). A passing check never publishes on its own authority.
8. **Speak MacPorts.** Commit subjects, the PR body, and the checks follow the MacPorts guide, the commit-message wiki, the PR template, and CI's actual behavior.
9. **Every refusal carries the way forward.** It names the obstacle, what was kept, and one concrete next command.
10. **Git works without dockhand.** Dockhand's records enrich branches rather than owning them. Both workspace styles, managed worktrees and your own checkout, have the same capabilities.

## 3. The model

This section is the consolidated contract. The v3 layers were rebuilt with a fresh database (`~/.dockhand/dockhand.db`) and no migration from v2. A v2 `state.db` is left on disk untouched and never read. Within v3, a newer dockhand migrates the database forward. Commands and settings still change without aliases: a removed one is refused, naming its replacement.

### The nouns

| Noun | What it is |
| --- | --- |
| **branch** | Stable identity (an ID independent of the ref name), the Git branch that locates it, its base, title, lifecycle, and at most one PR. |
| **revision** | An immutable candidate: a source tree and base. Either a **commit** (committed source) or a **snapshot** (captured working files, numbered per branch). The changed scope derives from it. |
| **verification plan** | For one revision: the frozen coverage, exclusions with their reasons, prerequisites, and environment choices. |
| **provider** | A way of building: `tart` (a clean VM per release), `local` (a MacPorts prefix on this Mac), `github` (MacPorts' CI workflow in your fork), or `command` (your own script). |
| **run** | One accepted check request: a plan, and the providers and releases it asked for. It has an ID that people type (`check-42`). |
| **guest execution** | For one run, provider, and release: the single owner of that environment. It holds per-target checkpoints. |
| **target result** | What happened to one target, in one configuration, with particular inputs. It is reusable only while those inputs still apply. |
| **assessment** | What upstream's change means for one target a revision changes, in one context, from its base to the revision: what was inspected, what couldn't be, and the concerns a passing build can't settle. It rests on readings of both versions' sources and on the port's evaluated facts, and applies only while they do. |

On screen, a person needs only branch, port, check, run, provider, and PR. The others appear with `-v` and in JSON.

### Scope, targets, and the plan

- **Derived scope.** This follows MacPorts CI's rule. A change to a `Portfile` or under `files/` marks its directory (`git diff --name-only master...`). The targets are every subport of each changed directory, less those `replaced_by`, `known_fail`, or unsupported on the platform, in dependency order. Commits never enter into it.
- **Target kinds.** A target is **revision-only** only when its Portfile diff touches nothing but `revision` declarations (shared code included) and nothing under `files/` changed. This is proven from the source, never from evaluated metadata alone. Anything unknown counts as substantive.
- **The plan keeps four things apart:**
  - the changed scope;
  - CI eligibility per environment, with the reason for every exclusion;
  - the selected coverage (`--only`, `--also`);
  - the prerequisites that coverage needs.

  The scope and the selection are the branch's. Everything else is each environment's own plan, from its own evaluation of the ports: what it builds, in its own dependency order, what each target needs there, such as Xcode, and why it doesn't build the rest. A port an environment doesn't define is excluded there, whichever other environment defines it. Dependencies that run opposite ways on two releases are no cycle, since each builds in its own order.

  A changed prerequisite is always included, so `--only appB` still builds the changed `libA` it depends on, and `appB` is blocked if `libA` fails. An old binary is never substituted. A target whose evaluation fails leaves the plan unresolved; it is never dropped as ineligible.
- **Execution.** Each provider and release gets one guest execution. It follows MacPorts CI's order: lint every target, then for each target in the environment's dependency order, activate exactly its dependencies, fetch, checksum, install, and test (advisory). Each target's result is checkpointed as it finishes. A target whose changed dependency failed is **blocked**, not failed. Infrastructure failures are retried; verdicts never are (decision 30).
- **Evidence.** A result is reused per target when every recorded input still matches: the observations evaluation made, negative ones included, and the digest of every archive it consumed (decisions 28 and 44).
- **Assessment.** Every target a revision changes is assessed against the revision's captured base, whoever changed it: an update, a hand edit, or someone else's pull request. It is the net change that's assessed, never the edits that made it, so a license changed and changed back holds nothing, and a later edit to the source or to a provider is assessed again. A concern the base already had is said, not held; one the candidate introduces, or one that couldn't be checked, holds. The expensive part, reading each version's source, is kept by what was read; the rules are applied again wherever an assessment is read, and it's collected only by commands acting on a request, never by `status`. A check whose assessment can't finish still completes, with the assessment incomplete. A Git-fetched port is compared through its forge's archive of each commit; one with no forge to ask is not assessed, and says so.
- **Publication rule.** Every selected substantive target must pass. A failed revision-only target, or a failed extra from `--also`, can be acknowledged with `--accept <port>`, which is recorded and shown in the PR as "cause not established". So can a changed target that `check --baseline` failed the same way at the base, and the PR names that baseline. A baseline is evidence and never unblocks publication by itself.
- **Identity and names.** A branch dockhand creates is named `dockhand/<name>`, where `start <name>` supplies the name. Without one, it's named for what it does (the person's decision, 2026-10-02): `dockhand/jq-1.8.1` for an update, `-rebuild`, `-new`, or `-checksums`, with a short ID added only where that name is taken now, and `dockhand/<first port>-<short ID>` where there's nothing more to say. The name is never changed once a PR exists. A branch renamed with Git keeps its identity through `adopt`'s reconciliation.
- **Titles.** A `--title` given at any step is kept. With one substantive port and no title, the title is that port's commit subject. With several and no title, it's the update's subject where every other commit is a rebuild for it, and otherwise `submit` refuses until `--title` gives one. Dockhand never composes one, and never retitles an existing PR unless `--title` is passed.

## 4. Context and selection

The context is the branch you're working on. It comes from, in order:

1. An explicit `--branch <name>`, `-p <port>`, or `--pr <number>`. The `dockhand/` prefix is optional.
2. The worktree you're in, and the branch checked out there.
3. Otherwise, a command that changes something asks on a terminal and refuses with the choices in a script.

The first line of every command that changes something names the branch and directory. There is no persistent "current branch" shared between terminals.

**A port name picks a branch only where nothing else could be meant** (relaxed 2026-10-02, at the person's word). Two branches may both touch `jq`, and neither silently receives the other's next edit. `-p jq`, or an authoring command's port with no branch named or checked out, names the one open branch that changes jq, and says so on its first line. Where none does, an authoring command starts one and says so. Where several do, a terminal asks once which one, or a new one, and a script is refused with the list. `-b` takes only a branch's exact name, and output always prints exact names. `dockhand status --port jq` finds them. `--new` starts another beside one: `dockhand update jq --new` creates `dockhand/jq-1.8.1` and updates jq in it.

### Where branches live

- **Managed worktrees.** `dockhand start <name>` creates a branch from freshly fetched `master` in a **sparse worktree**, in `~/Source/macports-branches/<name>` by default, wherever the clone is (set by `worktrees` in the config). A sparse worktree holds `_resources` plus the directories of the ports the branch touches, and grows when you `edit` another port. It's a normal Git worktree that any editor can open. `dockhand path <name>` prints its path. Dockhand can't change the parent shell's directory and doesn't pretend to.
- **Your own checkout.** `dockhand start <name> --here` creates the branch in the checkout you're in. `dockhand adopt` tracks the branch you're already on, as it is. Adopting never moves or rewrites anything. `start --here` refuses to displace uncommitted work, and says how to keep it.

Both styles get the same editing, checking, tidying, and submitting.

## 5. Commands

Help teaches the short loop first.

| Purpose | Commands |
| --- | --- |
| Start or enter work | `setup`, `start`, `adopt`, `path` |
| Author | `bump`, `ship`, `update`, `checksums`, `revbump`, `create`, `edit` |
| Understand | `status`, `diff`, `impact`, `outdated`, `open` |
| Check | `check`, `logs`, `retry`, `artifacts`, `shell` |
| Prepare for review | `tidy`, `submit`, `rebase`, `review` |
| Keep work moving | `wait`, `cancel`, `serve` |
| Occasional | `archive`, `clean`, `config`, `decide`, `explain`, `undo` |

```console
$ dockhand --help
Prepare, check, and submit changes to MacPorts ports.

In your ports checkout, dockhand setup sets up, offering the GitHub login,
your maintainers line, and a clean macOS image for checks to build in. Then:

  dockhand update <port> --new    a branch with the port at its newest release
  dockhand check                  build what the branch changes
  dockhand tidy                   shape its commits for review
  dockhand submit                 open the pull request from your fork

dockhand bump <port> does all four asking nothing, and stops where you
should look. docs/usage.md is the guide, and dockhand status shows where
things are.
```

The command groups follow, as the table lists them. Someone else's pull request is taken up with `adopt --pr`, and looked over with `review`. The checks queued and running are listed by `status`, and `status --watch` keeps that view live.

Every authoring command but `edit` accepts `--plan`, which shows the intended edit and changes nothing on your branch, in your files, or on GitHub. A plan does say what it had to fetch or evaluate to find out.

## 6. Journeys

These are mock sessions. Port names are real. Versions, PR numbers, and durations are illustrative, and `libharbor` is fictional.

### 6.1 First run

```sh
cd ~/src/macports-ports
dockhand setup
```

`setup` doesn't require a GitHub login, a fork, or a provider before local work can start. It registers the checkout, asks where branch worktrees go, and reports what dockhand runs with: Git, MacPorts, the places a check can build, the GitHub login, and its records. On a terminal it then offers what's missing, each in turn and each declined by answering no: the GitHub login, the maintainers line the ports naming that login write, and last, off unless you say yes, the Tart image of this Mac's release, naming its download and disk cost first. Its `Next:` line names what to run, such as `dockhand outdated --mine`. Run it again at any time to check the setup.

### 6.2 The ordinary update

```console
$ dockhand start jq-update
Created dockhand/jq-update from master 4c1e2d0 (fetched just now)
Directory: ~/Source/macports-branches/jq-update
Next: cd "$(dockhand path jq-update)"

$ cd "$(dockhand path jq-update)"
$ dockhand update jq
jq-update · ~/Source/macports-branches/jq-update
jq: 1.7.1 → 1.8.1   (GitHub tag jq-1.8.1)
Updated version and checksums (1 distfile); revision reset to 0.
Changed: textproc/jq/Portfile
Next: dockhand check

$ dockhand check
jq-update · captured working files as snapshot 1
Provider: tart · macOS 26 arm64 · Command Line Tools 26.1
Coverage: jq, default variants

  jq    lint ✓   fetch/checksum ✓   build/install ✓   tests ✓     4m12s

Passed for snapshot 1.
Next: dockhand tidy --branch jq-update

$ dockhand tidy
Proposed commit
  jq: update to 1.8.1
Includes textproc/jq/Portfile
Review diff [d] · Change groups [g] · Edit message [e] · Apply [a] · Cancel [q]
> a
Created 1 commit (checkpoint tidy-1). The files are unchanged, so the check still applies.

$ dockhand submit
jq-update · ready to submit
  Title    jq: update to 1.8.1
  From     ada/macports-ports:dockhand/jq-update
  To       macports/macports-ports:master
  Commits  1, follows MacPorts' commit rules
  Checks   passed on tart macOS 26 arm64 for this commit's tree
  Upstream compared; no license, build file, or dependency changes
  Other PRs  none open for jq
Preview [p] · Edit description [e] · Submit [s] · Cancel [q]
> s
Opened #34901  https://github.com/macports/macports-ports/pull/34901
```

The same thing as one line, for someone who knows what they want:

```sh
dockhand bump jq
```

`bump` is `ship jq --yes`: the one-shot, where `update` is the edit alone, a step at a time, looking and editing along the way (added 2026-09-27, 5725d673). Since nobody looks before it submits, it holds its pull request for what holds `serve`'s (§11), and exits 3 naming the `submit` that finishes it. Before the edit, it stops with nothing changed when the check has nowhere to build, or the port is already at the release; an open branch already changing the port is resumed. It never ticks the template's tested checkboxes, which only `--tested-binaries` and `--tested-variants` do. Its `--json` result is `ship`'s: each change's update, with tidy's, check's, and submit's inside it, as far as it went, and `submit.held` saying why a held submission waits.

`ship jq` is the same, showing one plan and asking once. It tidies the update's single authoring edit, checks it, and submits that exact commit once the check passes; `--to commit` or `--to check` stops sooner. Without a terminal, running it is the consent, and it holds where `bump` does. `--build`, `--on`, `--tested-binaries`, and `--tested-variants` go to the check and the pull request, and where to check is settled before the edit, so a mistaken `--on` changes nothing. A plain `submit` never checks by itself.

### 6.3 When dockhand can't edit the Portfile

```console
$ dockhand update openjdk21
openjdk21-update · ~/Source/macports-branches/openjdk21-update
✗ can't update openjdk21 by itself [hook-exec]
  java/openjdk21/Portfile:48: its pre-fetch hook runs `exec`, and dockhand won't guess what that changes.
  Kept: the branch, unchanged.
  Edit the version and its checksums yourself; `port checksum openjdk21`, after the version's edit, says what its archives have:
    dockhand edit openjdk21
```

This is the job `port bump` has done since MacPorts 2.6. After a hand edit of `version`, `dockhand checksums` fetches the new distfiles, writes the checksums, and resets `revision`, all inside the branch.

### 6.4 A new port from a URL

```console
$ dockhand start rift
$ cd "$(dockhand path rift)"
$ dockhand create https://github.com/rift-dev/rift
rift 0.4.2 · Rust (Cargo.toml) · GitHub says MIT · "Fast structural diff for config files"
? category [devel]: textproc
Created textproc/rift/Portfile from the github and cargo PortGroups
  cargo.crates: 143 crates, from cargo2port
  checksums: 1 distfile + 143 crates
  Unconfirmed, marked in the file: license (from GitHub's detection), long_description
Next: dockhand edit rift, then dockhand check
```

`create` takes a GitHub project's URL, or a registry name (`pypi:`, `crates:`, `go:`) whose source is on GitHub. It fills in what it can observe and marks what it guessed, since it never presents a guessed license or maintainer as fact. The maintainer line comes from your config (`{example.org:ada @ada} openmaintainer`). The new files are added to the branch's index, so a first check includes them. The file follows `port lint --nitpick`'s layout: the modeline, four-space columns, and `rmd160 sha256 size` checksums. Because the guide asks new submissions to fix `--nitpick` findings, a check lints a new port with `--nitpick`, and its warnings hold the submission until they're fixed or accepted.

A handwritten Portfile is just as much a first-class path. `check` builds it the same way whether or not `create` made it.

### 6.5 Checksums and a stealth update

```console
$ dockhand checksums croc --new
croc-checksums · ~/Source/macports-branches/croc-checksums
croc 10.2.4 · the distfile changed upstream without a new name (stealth update)
  was   sha256 1f3a…c2d9   size 7,114,391
  now   sha256 9b0c…77e1   size 7,114,508
  inside the archive: 1 file differs, go.sum (+2 −2)   (dockhand diff --source for all of it)
Updated checksums, revision 0 → 1, and dist_subdir croc/10.2.4_1, so mirrors keep both archives.
The source changed, so the revision is bumped; --no-revbump leaves it, for a change that needs no rebuild.
```

The source changed, so by default the revision is bumped and `dist_subdir` follows it: `${name}/${version}_${revision}`. Each stealth update then gets a directory of its own, with no count to keep. `--no-revbump` leaves the revision alone, for a re-rolled tarball whose contents didn't really change. It then numbers the directory instead, `${name}/${version}_1`, which is the MacPorts guide's recipe. Following the revision without a bump would put the new archive where the old one is. A `dist_subdir` already numbered keeps counting, even with a bump: `_${revision}` at revision 1 would be the `_1` the earlier archive is already in. A later version update removes either form, where every archive of the new version has a name of its own, by MacPorts' fetch plans for both. One whose name stays the same, as yq's man page's does, still needs the line to keep its versions apart on the mirrors, so it stays. (Revised 2026-09-25, f07b59f1, and 2026-09-29, be12c80a.)

### 6.6 Revision bumps

The reason is the subject, because that's how maintainers write it: 91 of 93 recent upstream rebuilds put the reason in the subject.

```console
$ dockhand revbump gdal inkscape --subject "rebuild for poppler 25.09.0" --new=poppler-25.09
poppler-25.09 · 2 ports
  gdal       revision 2 → 3
  inkscape   revision 0 → 1
Recorded the subject for tidy: "<port>: rebuild for poppler 25.09.0"
```

Without `--branch` or a worktree, `revbump` starts a new branch, because a rebuild has its own reason. A shared `revision` line is bumped for all the subports it covers, and the plan lists them.

### 6.7 A library and the ports that link it

```console
$ dockhand start libharbor-2 && cd "$(dockhand path libharbor-2)"
$ dockhand update libharbor 2.0 --revbump-dependents
libharbor: 1.9 → 2.0
Direct library dependents, from the index at 4c1e2d0:
  harbor-cli  harbor-viewer
Revision bumped both; subject "<port>: rebuild for libharbor 2.0" recorded for tidy.
  · 1 of them has another maintainer; the PR will mention them (submit --skip-notification keeps it quiet)

$ dockhand impact libharbor
Changed ports     libharbor, harbor-cli, harbor-viewer
Other dependents  harbor-tools (build), harbor-docs (runtime); candidates to look at, not proof of anything
Shared files      none
Last 6 libharbor updates on master also rebuilt dependents.

$ dockhand check --also harbor-tools
libharbor-2 · snapshot 4 · tart macOS 26 arm64
Order: libharbor → harbor-cli, harbor-viewer · also harbor-tools

  libharbor       ✓   6m02s
  harbor-cli      ✓   using the changed libharbor
  harbor-viewer   ✗   build failed
  harbor-tools    ✓   extra coverage, no source edits

harbor-viewer did not build against libharbor 2.0. The failure happened while compiling harbor-viewer; libharbor built.
Your files and the other three results are kept.
Inspect: dockhand logs check-42 harbor-viewer
```

There is no primary port with secondary ports attached. Revision-bumped ports are changed ports like any other, and `--also` adds unchanged ports as extra coverage. `--revbump-dependents` bumps direct library dependents only, taken from the index, and `--plan` shows the list first. `--except <port>` takes a port out of either list.

### 6.8 A failed check

```console
$ dockhand logs check-42 harbor-viewer
…
src/render.c:212: error: too few arguments to function 'harbor_open'

$ dockhand check --baseline --only harbor-viewer
harbor-viewer at master 4c1e2d0 (libharbor 1.9) · tart macOS 26 arm64
  ✓ builds at the base. This branch's result differs; the cause isn't established.
```

A baseline reports what happened in each run and nothing more. It is off by default: `--baseline` runs one, and `check.baseline = true` runs one after every failed check.

When a port fails while building, installing, or testing, and the base has it, the failed check names the command that builds it at the base. It doesn't for a lint, fetch, or checksum failure, which comes from the branch's own edit, nor for a port the branch adds.

A baseline is always dockhand's own build. MacPorts' buildbot history is never used as one, nor shown beside one. That is the person's decision of 2026-09-27, in [decisions](decisions.md).

You fix the failure, then check again:

```console
$ dockhand edit harbor-viewer          # add files/patch-libharbor-2.diff, list it in patchfiles
$ dockhand check
libharbor-2 · captured working files as snapshot 5
  ✓ reused libharbor, harbor-cli, harbor-tools: their inputs are unchanged
  harbor-viewer   ✓   3m40s
Passed for snapshot 5.
Next: dockhand tidy --branch libharbor-2
```

Only `harbor-viewer` is rebuilt, because the other results still apply. `retry check-42` repeats the pinned request exactly; `check` captures newer edits.

### 6.9 Tidying a stack of commits

This is the feedback maintainers most often give newcomers. Here, someone made a branch by hand, pushed six commits, and was asked to squash them:

```console
$ git switch update-jq && dockhand adopt
Adopted update-jq: 6 commits above master 4c1e2d0, changing jq, with PR #34905.

$ dockhand tidy
update-jq · 6 commits

Suggested series
  1  jq: update to 1.8.1
       combines "Update jq", "fix checksums", "oops", "address review", "bump revision", "typo"
       keeps: Closes: https://trac.macports.org/ticket/71234
Also noticed, for you to fix:
  ✗ revision is 1 after a version update; MacPorts expects 0

Review diff [d] · Change groups [g] · Edit message [e] · Apply [a] · Cancel [q]
> a
Created 1 commit. Checkpoint tidy-3 keeps the old history (dockhand undo tidy-3).
The PR still shows 6 commits until you submit; submit will replace its history (with lease).
```

For example:

- a single-port update with six corrections becomes one commit;
- a new port and the prerequisite port it needs stay two commits;
- a bug fix and an unrelated maintainer change in one Portfile stay two;
- a PortGroup change and the ports it touches can be one commit, if that's the review unit the author intends.

Details of the rules are in [§8](#8-tidy-and-what-macports-asks-of-commits).

### 6.10 Review feedback

```console
$ dockhand
jq-update · #34901 · changes requested by @ryandesign, 2h ago
  "Please also drop patch-configure.diff; it was upstreamed in 1.8.0."  (textproc/jq/Portfile:31)
  MacPorts CI  ✓ macOS 14  ✓ macOS 15  ✓ macOS 26
Next: dockhand edit jq

$ dockhand edit jq                 # remove the patch and its patchfiles line
$ dockhand check && dockhand tidy && dockhand submit
…
  ✓ updated #34901: 1 commit, replacing its history (the remote head was the one last seen)
  ? ask @ryandesign to review again? [Y/n] y
```

`submit` says whether it will append commits or replace the remote history. If someone else pushed in the meantime, it stops and shows the comparison. It never force-pushes over their work.

### 6.11 Someone else's PR

```console
$ dockhand adopt --pr 34905
Adopted pr-34905: "Update jq" by @newcontrib, 6 commits, jq; maintainers can edit.
Directory: ~/Source/macports-branches/pr-34905
```

Adopting someone else's PR lets you inspect it and work on it locally. It doesn't assume permission to push to their branch. `submit` names that destination and checks your access before pushing anything.

`review` reports what a reviewer would say, and posts it only if you ask:

```console
$ dockhand review 34905
review of #34905, as @ada (write access to macports/macports-ports)
  summary                  6 commits for one change to jq; MacPorts asks for one per logical change.
                           To squash: `dockhand tidy`, or `git rebase -i master` and push with --force-with-lease.
  commit a1b2c3d           subject "Update jq" should be "jq: update to 1.8.1"
  textproc/jq/Portfile:7   revision should be 0 after a version update
? post as: c comment · r request changes · e edit first · n don't post  r
✓ review posted on #34905: changes requested, 1 inline comment
```

`review` evaluates the PR's ports and applies §8's commit rules, leaving `port lint` and the build to MacPorts CI. `--check` also builds the PR. It posts nothing without showing the text and asking. "Request changes" is offered only when your account has write or triage access to macports/macports-ports, and dockhand says why when it isn't offered. `--markdown` prints the text for pasting. When you review a PR again after it changes, dockhand says which earlier findings are resolved instead of repeating them.

### 6.12 Working through your ports

This journey is the core of the maintainer's day.

```console
$ dockhand outdated --mine
  PORT        NOW       NEWEST    DOCKHAND CAN
  croc        10.2.4    10.2.5    update
  fd          10.2.0    10.3.0    update
  jq          1.7.1     1.8.1     update
  xan         0.52.0    0.53.0    update (cargo2port)
  openjdk21   21.0.8    21.0.9    edit by hand
5 of 212 ports have newer releases · 3 couldn't be checked (-v says why)

$ dockhand ship --mine --to check      # a branch, a commit, and a check for each
$ dockhand ship --passing              # later: the branches that passed, to their pull requests
```

Before creating anything, `ship` shows one plan with a row for each port, one branch each, since unrelated ports go in separate PRs, and says which it leaves, such as openjdk21, which needs a hand edit. It asks once. A batch carries each change to its check, queued, before it sees any check through: `serve` runs them side by side where it runs, and they run here one by one where it doesn't.

`ship --passing` takes each branch whose check passed for exactly what would be submitted, a committed tree with no pull request of it yet, and shows them on one plan with each one's submission preview in brief and its holds, for one answer (D11). The `!` lines compare the old and new upstream archives, which dockhand already fetched for the checksums, looking for changed license files, build files, and declared dependencies. Those are what a reviewer would ask about, and what a passing build can't catch. A single `submit`'s preview shows them too.

`serve` can do the morning's preparation by itself with `serve.ship_to = "check"`. It then finds new releases of your ports, creates and tidies a branch for each, and checks them, so `ship --passing` is all that's left. By default (`list`) it only counts them. `serve.ship_to = "submit"` goes one step further ([§11](#11-serve-the-queue-and-instance-coordination)).

### 6.13 After the merge

`serve` looks at open PRs every few minutes, and so does `status --refresh`. A merged branch is recorded as merged and stays searchable. `clean --merged` previews removing its worktree, local branch, and fork branch, each only while it still holds the merged commit. Cleanup also runs automatically on decision 36's schedule. Dirty directories, and work that has advanced beyond what was merged, are kept. `archive` hides inactive work without touching its PR. `archive`, `clean`, and `cancel` never mean the same thing.

## 7. Check

### What a check captures

Inside a branch's worktree, `check` captures the final working contents of tracked files, including staged additions and tracked deletions, as a numbered snapshot of that branch. Options change what it captures:

- `--staged` checks the index.
- `--head` checks the committed tip.
- For a `--branch` outside its own worktree, the default is its committed head. If that worktree has edits, the check needs `--head` or `--working-tree`, rather than choosing one for you.

Untracked files are listed and excluded. `--include <path>` adds them to the capture without staging them. An untracked patch that a changed Portfile references is a missing-input error, never a quiet pass. Unresolved conflicts prevent a capture, and a capture that sees files change while it reads retries or stops.

### Plan, coverage, and providers

```console
$ dockhand check --plan
Source      working files in libharbor-2 (would be snapshot 6)
Changed     libharbor, harbor-cli, harbor-viewer
Also        harbor-tools
Provider    tart · macOS 26 arm64 · default variants
Order       libharbor → harbor-cli, harbor-viewer · harbor-tools
Excluded    harbor-viewer-legacy: supported_archs x86_64 only
```

Coverage options:

- `--only <port>` narrows the check to fewer changed targets, and adds their changed prerequisites back, visibly.
- `--also <port>` builds unchanged ports against the branch, such as the dependents `impact` lists.
- `--variants` applies to a single selected target.

A narrowed check never quietly shrinks what `submit` requires.

`--on <provider>[:<releases>]` selects where the build runs. Examples are `--on tart:sonoma,tahoe`, `--on tart:all`, `--on local`, `--on local:/opt/macports-test`, and `--on github`. A bare release name means Tart. Without `--on`, a build profile says where: `--build` names one, `build.default` is the default (`host`, this Mac's release), and `build.unattended` serves work nobody looks over, `serve`'s and `bump`'s. Several `--on` flags mean all of them must pass, never one of them (decision 29). Switching a queued or running check to another provider is explicit: `check --on github --replace`.

| Provider | Builds in | Clean each time | What its result can claim |
| --- | --- | --- | --- |
| `tart` | a fresh clone of a prepared VM image per release | yes | CI's order, declared tests, any release with an image |
| `local` | a MacPorts prefix on this Mac, `/opt/local` or one of yours | no, unless the prefix is listed in `clean`; says how many ports were active | this Mac's release, among what you have installed |
| `github` | MacPorts' CI workflow in your fork | yes | only what CI does: default variants, `port lint` without `--nitpick`, the built-in test phase, the runners' releases |
| `command` | whatever your script does | as it reports | labelled "reported by <name>"; dockhand can't vouch for it |

`local` builds in `/opt/local`, or the prefix `[providers.local] prefix` or `--on local:<prefix>` names; `--on local:/opt/local --on local:/opt/macports-test` builds in both, each its own environment. Dockhand never installs MacPorts: any other prefix is one you made with MacPorts' own source install. A prefix whose MacPorts predates `port snapshot` (2.9) is refused. The guest program Tart's clones run builds the targets there, under the prefix's own `port-tclsh`, in CI's order. MacPorts reads the branch through a `macports.conf` of the check's own, named by `PORTSRC`, whose `sources_conf` lists the staged tree first and the prefix's own sources after it; the prefix's `etc/macports` is never written.

It builds as-is: nothing is deactivated, and how many ports were active, and any other package manager found, are facts of the build, which the Tested on line names ("built in /opt/local on the author's Mac with 143 ports active"). `port snapshot` records the prefix before anything changes. Afterwards dockhand activates again each port whose active version changed and deactivates what the check activated, leaving the new images inactive and unrequested for `port reclaim`. `check --keep`, or `[providers.local] keep = true`, leaves the branch's versions active instead. A prefix listed in `[providers.local] clean` is treated as dockhand's own: each target builds there with all but its dependencies deactivated, as in a clean VM, and the rest is put back after. A check that ends before it puts the prefix back leaves it changed; status says so, with `sudo port restore --snapshot-id N`, and the next check there puts it back first.

Where you can write the prefix's registry, as in a prefix installed without root, nothing needs `sudo`. Otherwise a check asks for your password once and runs the build as root. serve doesn't check in a prefix unless `[providers.local] serve = true`, which needs a sudo rule with no password: root for anything a Portfile does. A pull request you adopted is refused there, since someone else's Portfile would run as root on this Mac. MacPorts' binary archives serve `/opt/local` alone, so in any other prefix every dependency builds from source, and the plan says so.

A `command` provider receives a request file (the revision as a Git bundle, the targets in order, the release, the variants, and the test policy) and writes a result file with each target's phases. Configuring any provider that pushes somewhere shows that push when the check is accepted: "only checking" never hides a write to your fork.

### Results

`check` covers lint, fetch and checksum, build and install, and declared tests. Each target reports one of passed, failed at a phase, blocked by a failed changed dependency, unmet (the environment can't build it, such as a port needing Xcode where there is none), not run, or interrupted (its environment was lost as it built, so another attempt builds it again).

Tests are advisory, as in MacPorts CI: "build passed; tests failed (advisory)" is shown as exactly that. `--tests required` makes a test failure decisive.

Independent targets continue past a failure. Interruptions, infrastructure trouble, and a failed build are distinct outcomes. The result says which snapshot or commit it covers, and `status` says when the current work has moved on: "passed for snapshot 3; the files have changed since".

A result stands for the environment it ran in, and an environment has an identity by origin (decision 28). For Tart, that is the vanilla image's digest, the MacPorts, Command Line Tools, and Xcode setup installed, and the versions of setup's steps and of the guest program that verifies each port. Each provider run records it as it begins. An image made again from another source, or with other tools, is another environment. Its earlier results stop counting, and `status` says so: "check-3 passed, but tart macOS 26 (Tahoe) arm64 with Xcode has been made again since, from another source or with other tools; jq must be built there again". Where a provider can't say what an environment is, a result stands as it was.

Each result also keeps what its build read, by content: the environment's identity, the target's directory and `_resources` by tree, the variants asked for, and every port active as it built, with its archive's digest. It keeps the digest of the archive its build made too, and Tart's builds keep the archive itself, beside the database, once it arrives whole and matches that digest: only then is it ready for dependents (decision 44). Automatic cleanup removes a kept archive once no open branch's check names it, and none has within `cleanup.after`. A later check reuses such results rather than build again: a target whose build would read what an earlier build of it read, in the environment as it is now, reuses that build's result (decision 28). Where every target does, nothing starts there. Otherwise the rest build. A reused target one of them needs is installed in the guest from its kept archive, which the guest's MacPorts fetches from an archive site of MacPorts' own kind, signed with dockhand's keys; without a kept archive, that target builds too. A target needs what the plan says it does, and what was active as it last built, which it may reach through ports the branch doesn't change. A retry installs what an earlier attempt finished the same way. The pull request names the run that built each port, "reused in check-N". `check --fresh` builds everything. A provider that can't say what its environment is, as the github and command providers can't yet, reuses nothing there, and a check says so where another of its environments could.

## 8. Tidy, and what MacPorts asks of commits

`tidy` answers the question: **what should a reviewer see as the separate changes in this branch?**

It proposes a series of commits, built from these hints:

- what the authoring commands recorded (the kind of change, and any subject you gave);
- file paths;
- existing commit messages;
- which commits read as follow-ups to earlier ones.

Hints are not proof. Mixed commits and overlapping hunks produce a plan for you to review. Dockhand admits uncertainty rather than inventing a polished but wrong series, and history that already has a good shape passes through unchanged.

A plan is **unambiguous** when every group is one port directory's edits from a single authoring command, with a subject dockhand wrote. That's the only kind `--yes`, a script, `submit`, `ship`, `bump`, or `serve` may apply without review. Other options:

- `tidy --squash --message "…"` squashes the whole branch explicitly, saying which commits and edits it includes.
- `tidy --plan --out <file>` saves a reviewable plan, and `tidy --apply <file>` applies it. A saved plan is bound to the base, the tip, and the captured working state, and goes stale if any of them change.

Tidy guarantees:

- The final tree is the working files as they are, but for the lint fixes dockhand makes on the lines the branch changes, such as trailing whitespace or a lone `openmaintainer`. `check` makes the same fixes before it captures, so content evidence still applies. `tidy --restyle`, which restyles whole Portfiles, does so in a last commit of its own.
- Human authorship and existing trailers are kept. Combining commits by several authors asks you to choose the attribution.
- Before applying, it saves a named checkpoint of the old refs and any captured index or working state. `dockhand undo <checkpoint>` restores it, after checking for newer work.
- Merges are never flattened without a specific, reviewed choice.

### History changes as complete transitions

`tidy`, `rebase`, and `undo` each change a branch's history as one transition:

- **The branch's lock.** Each holds a per-branch lock throughout, so no other dockhand changes that history meanwhile. The lock is a file lock the operating system releases when its holder ends, and the Git commands it runs hold it with it.
- **Nothing moves until the new commits exist.** A tidy composes its commits, and a rebase replays the branch's commits onto master with Git's merge machinery (`git merge-tree`), before any ref moves. A rebase that conflicts changes nothing.
- **The checkpoint is recorded first.** A tidy or rebase records its checkpoint as *prepared*, with the head it replaces and the one it writes, then makes its Git change, guarded by compare-and-swap, then settles the checkpoint as *applied*, or *abandoned* when the change couldn't be made. An undo makes its change, then records it. No transaction calls Git.
- **An uncertain commit is read back.** A commit whose outcome the store can't know is read back before anything more is done. A Git change once made is never undone.
- **A stopped process is finished by the next one.** The next `tidy`, `rebase`, or `undo` of the branch settles what a stopped one left, from what Git shows. A prepared checkpoint whose change was made, the branch at or past the head it wrote, is applied. One whose change wasn't is abandoned, and the refs it made are removed. An undo that stopped before recording itself, the branch back at the checkpoint's old head, is recorded, and a rebase's base goes back with it.

This is the checkpoint doing the work of an operation record, rather than a general workflow engine.

### The commit rules

`tidy` writes subjects by these rules. `submit`'s preview and `review` apply them, and `dockhand explain <code>` quotes the MacPorts source behind each.

| Rule | Finding |
| --- | --- |
| Subject names the port | ✗ unless it starts with `port:`, or `port1, port2:` for several ports |
| Subject is specific and short | ! "update to latest", "fix"; ! over 60 characters (wiki: aim for 50–55) |
| Body wraps at 72 | ! |
| Tickets as full URLs | ! `#71234` rather than `Closes: https://trac.macports.org/ticket/71234` |
| No follow-up commits | ✗ a commit that only corrects an earlier one in the branch |
| No merge commits | ✗ `rebase` instead |
| Revision after an update | ✗ not 0 after a version change |
| No rebuild for a build fix | ! a revision bump in a commit that only changes how the port builds |
| Version order | ✗ the new version sorts lower under `vercmp` without an `epoch` change |
| Obsoleting | ✗ `replaced_by` without a version, revision, or epoch change |
| Other PRs | ! an open PR changes the same port |

`port lint` is the check's, as it is CI's (§7), and `impact` lists the ports that load a changed PortGroup.

Subjects dockhand writes:

| Change | Subject |
| --- | --- |
| New port | `rift: new port` |
| Update | `jq: update to 1.8.1` |
| Rebuild | `gdal: rebuild for poppler 25.09.0` (your `--subject`; never invented) |
| Checksums | `croc: update checksums after a stealth update`, or `croc: update checksums` |

Trailers go last: `Closes:`, then `See:`, then dockhand's `Generated-By:`.

## 9. Submit

`submit` opens or updates the branch's one PR. Its preview shows the commits, the head and base repositories, the title, the description, the coverage, the commit-rule findings, and any other open PRs for the same ports.

- **`submit`** needs committed source and results that apply to its tree, under the publication rule in §3. If the worktree has edits, it commits them first, as `tidy` would: a plan of dockhand's own edits goes unasked, and one with yours is reviewed on a terminal and held without one. `--head` leaves them out. Evidence from the working tree never authorizes pushing a different committed tree.
- **`submit --draft`** allows unfinished or failing checks, and shows them in a draft PR, which gets MacPorts CI started early. The source must still be committed. `submit --ready` takes the PR out of draft.
- **`submit --accept <port>`** acknowledges a failed revision-only target, or a failed extra from `--also`, for this exact revision. It's recorded, and shown in the PR as "cause not established". A failed substantive target can be accepted only where `check --baseline` failed it the same way at the base, and the PR names that baseline; otherwise the way to share one is `--draft`.
- **`submit --no-check`** publishes without any check. The PR says no local build ran, and MacPorts CI becomes its only check.
- **`ship -b <branch>`** checks the branch and submits that exact commit once its check passes, and **`ship --passing`** is the batch review from §6.12.

The PR body is written in the template's own sections, and ticks only what dockhand has evidence for.

- **`#### Description`**: for one commit, its body. For several, a table of commit, port, and change.
- **`###### Type(s)`**: `bugfix`, `enhancement`, or `security fix`, ticked when stated (`--type`), `enhancement` for an update dockhand made, and `security fix` when a commit cites a CVE. MacPorts' bot detects "update" from a title containing ": update", and "submission" from a new Portfile, by itself.
- **`###### Tested on`**: the template's lines for each release built (`macOS 26.1 25B78 arm64`, then Xcode or the Command Line Tools version). Each names the provider and what its result establishes: "built in a clean VM", "built in /opt/local on the author's Mac with 143 ports active", "MacPorts CI workflow in ada/macports-ports" with the run's link, or "reported by <name>". A grid below shows each port against each provider and release, including anything accepted, baselined, or not built locally.
- **`###### Verification`**:
  - `port lint`, `sudo port test`, and install are ticked from recorded results. Dockhand builds from source as CI does, without trace mode (decision 28), so the `-vst` item is ticked with that stated, never silently.
  - "Commit Message Guidelines" and "squashed and minimized" are ticked when the commit rules passed on what's being submitted.
  - "Other open pull requests" is ticked when dockhand searched and found none; otherwise the findings are linked.
  - "Tested basic functionality" and "most important variants" are asked, and a yes is recorded as your statement. `--variants each` evidence ticks the variants item by itself.
  - Items that don't apply are removed, as the template's comment asks.

`--skip-notification` adds `[skip notification]`, which stops MacPorts' bot from mentioning maintainers. `submit` suggests it for a batch of trivial rebuilds.

A description a person has edited is kept. Only the parts dockhand owns are regenerated, each only while its text is still what dockhand wrote: the Type(s), and everything from Tested on through Verification. The Type(s) only gain ticks that way: one dockhand ticked stays ticked, though a person's change folded in since means dockhand wouldn't tick it now. `--type` is the person naming the Type(s), so it replaces them however they read. In a description that leaves them out, with Tested on alone, it adds them before Tested on; without it, such a description stays without them.

## 10. Status and the attention list

With no arguments, `dockhand` shows status. Inside a branch, that's the branch's own status. At the repository root, it starts with what needs you:

```console
$ dockhand
Needs you
  ! jq-update       #34901 changes requested by @ryandesign (2h)        dockhand edit jq
  ✗ libharbor-2     harbor-viewer failed to build (check-42)              dockhand logs check-42
  · fd-2mxa         passed; waiting for you to submit                     dockhand submit --branch fd-2mxa
  ! rift            snapshot 2 passed; files changed since                dockhand check --branch rift

BRANCH          PORTS  WORK                CHECKS                    PR
jq-update           1  1 commit            passed for this commit    #34901 changes requested
libharbor-2         3  edits after check   1 failed, 3 passed        —
croc-7hq2           1  1 commit            passed                    #34902 open, CI ✓
gdal-rebuild        1  1 commit            passed                    #34880 maintainer timeout ends in 41h
rift                1  draft files         passed for older work     —

serve: running (pid 4711) · queue: 2 runs
```

- **`status --attention`** prints only the "needs you" list, for a prompt or a script. Each row ends with the one command that moves it forward.
- **`status --port jq`** finds every branch touching jq.
- **`status --all`** includes merged and archived branches.
- **`status <branch>`** shows one branch: its commits, targets and their results by provider and release, the plan's exclusions, and the PR.

Each column answers one question. No single "done" field encodes source state, checks, and review together. Observed forge state shows its age.

Below the branches, `status` lists the checks queued and running, and the dockhand processes working on the checkout.

`dockhand status --watch` is the same view live, with keys for check, logs, tidy, and submit that use each command's own semantics and confirmations. It observes only: `serve` does the work, so opening the view never starts anything. `--plain` prints its events as lines instead, for SSH sessions, scrollback, and screen readers.

## 11. Serve, the queue, and instance coordination

### What a person sees

```sh
dockhand serve                 # drive the queue in this terminal until interrupted
dockhand serve --install       # a launchd agent: starts now and at every login
dockhand serve --uninstall
dockhand serve --drain         # run what is queued now, then exit
dockhand serve --ship-to submit   # also open PRs for the branches it prepared that pass (see below)
```

| Situation | Behavior |
| --- | --- |
| `check` while `serve` runs | Submit the request and follow it here. Ctrl-C detaches; the run continues. |
| `check` with no `serve` | Run this request in the foreground, with the same runner, and say so. Ctrl-C stops it safely and keeps finished results; `retry` resumes. |
| `check -d` / `--enqueue` with no `serve` | Save the request and return, starting `serve --drain` in the background to run it, and say so. Exit 0 means saved, not passed; `wait` follows it. |
| `wait <run>` | Observe that exact run. With no `serve` running, run it here, as a foreground check would. |
| `serve` starts while a foreground check runs | The foreground command keeps its own run to the end; `serve` takes everything else. |
| A second `serve` | Stands by, says which process leads, and takes over when the leader dies. |
| `serve` or its Mac restarts | Reconcile active provider work before retrying anything; finished target results are kept. |

The queue holds immutable check requests, listed by `status`. Editing a branch after it was queued never changes what the request means. One check of a branch is queued or running at a time: `check` refuses another, and `--replace` stops it, keeping what it finished. People come first: work someone is attached to, or submitted by hand, runs ahead of work `serve` started by itself. Branches otherwise take turns, and each provider has its own capacity.

What `serve` does besides running checks (all of it shows in `status`):

- **Follows PRs.** Every few minutes it reads state, reviews, and CI for your open PRs, and turns changes, a conflict with master among them, into attention rows.
- **Handles your outdated ports** once a day, at `serve.outdated_at`, as far as `serve.ship_to` says:
  - `list`, the default: counts new releases of your ports in `status`.
  - `commit`: creates a branch with one commit for each new release.
  - `check`: also checks each one, on `build.unattended`'s profile, so the morning is `ship --passing`.
  - `submit`: also submits each that passes, as below.
- **Submits what passed, only with `serve.ship_to = "submit"`.** By default `serve` only builds and tests. With that setting, or `--ship-to submit` for one run, it opens a PR for each branch it prepared whose check passed; `--ship-to check` stops at the check for one run. Either way, `serve` says at startup, and in `status`, that it opens PRs for passing updates. Guardrails:
  - **Scope.** Only the branches `serve` itself created from new releases. Your own branches are submitted by you, with `submit` or `ship`; serve names the passing ones for you to ship.
  - **What passed.** The check must have passed on the exact committed tree being submitted, under the publication rule in §3, with no acknowledgements needed. A branch that needs `--accept` waits for a person.
  - **Held for a look.** A branch with any concern that holds, one its revision introduces or one that couldn't be checked, is not submitted, and lands on the attention list instead. Concerns are typed records, from the assessment of its revision (§3), its release's selection, the commit rules, and the search for other PRs; the `!` lines, JSON, and the attention list all read them. They hold for:
    - any `!` finding of the assessment, such as a changed license file, new dependencies a Python or Node project declares, changed build files, or a Go release a module-mode port's go.mod requires that its go.toolchain_min doesn't;
    - what couldn't be assessed, since what couldn't be checked waits too (D4, 2026-09-27): archives that couldn't be compared, a layout the reader doesn't understand, a Git port with no forge to ask, or a changed Python requirement whose provider's version can't be read;
    - a release selection that's uncertain;
    - any commit-rule warning;
    - another open PR for the port, or a failed search for one, since serve would otherwise open a second PR for an update someone already proposed.
  - **A daily limit.** At most `serve.submit_limit` PRs a day (10 by default). The rest wait for the next day, so a release wave doesn't land on reviewers all at once.
  - **Visibility.** `status` says "opens PRs for passing updates" on serve's line. Every PR it opens appears on the attention list as "opened by serve", and the PR body says that it was submitted without a person's review.
- **Cleans up** on decision 36's schedule.
- **Notifies** through macOS notifications, when a check finishes or a PR changes (`serve.notify`).

### How it works: sessions, leases, and a journal

The domain is still one Mac, one person, a few terminals, and one database. The database stays the only coordination medium: there is no socket and no network listener.

1. **Sessions.** Every dockhand process that runs work, or that watches, records a session row: a random session ID, pid, process start time, a start token, its kind (`serve`, foreground, or observer), dockhand version, schema, and a heartbeat every few seconds, written outside long transactions. Everything runs on one Mac, so a session's death is known directly. A pid that no longer exists, or exists with a different start time, is dead at once. The heartbeat covers a hung or sleeping process, and it counts as stale only by the writer's own recorded clock, and only after several missed beats, so a laptop waking from sleep doesn't flap.
2. **Fenced leases replace anonymous claims.** Every claim on a job, run, guest execution, provider reservation, or control application names its session and carries a fencing generation. Every write checks the generation, and a stale writer's late write is refused. When a session dies, its leases are void at once, instead of expiring at the step deadline ten or fifteen minutes later. External actions keep today's idempotency and reconciliation, because a fence can refuse a stale write but can't stop a paused process from making a late provider call.
3. **One leader.** `serve` takes the leader lease for its repository registration (decision 35 keeps one repository per engine). The leader drives all queued work. A foreground command without a leader leases only its own run. A second `serve` stands by and takes the leader lease when the leader's session dies. The live `status --watch` view is always an observer.
4. **Controls are applied by whoever can apply them.** `cancel` records its request. If a live session holds the run's lease, that session applies the cancel at its next step. If none does, the cancelling command takes the lease and applies it itself. A cancel never sits unapplied because nobody is driving.
5. **An event journal for observers.** Every transition, provider milestone, capacity wait, retry, and problem is appended to an events table, tagged with run, branch, target, session, and time, in the same transaction as the state change it describes where there is one. Today's driver-local progress lines ("cloning image", "waiting for the guest") become events. `status`, `status --watch`, `wait`, and a following `check` tail the journal by sequence number, so every terminal sees the same narrative: `wait` and `check` a run's events, by the run, and `status --watch` from the newest on. Build logs stay files beside the database. Automatic cleanup prunes events, and sessions that ended or went quiet, older than `cleanup.after`, sparing a session a lease names (revised 2026-09-28, 146eb75b).
6. **Version skew.** A session records its version and schema. `serve` refuses to lead a database whose schema is newer than it knows. An observer renders unknown event kinds generically, rather than failing. `serve --install` after an upgrade restarts the agent on the new build.
7. **Across databases.** Two database files still can't see each other's leases. Dockhand keeps its own Tart home (decision 42). Several databases on one Mac are a development setup's alone, so nothing more coordinates them ([decisions](decisions.md)).

A socket-based client and service stays unbuilt. The reason is in [§15](#15-where-v3-pushes-back).

## 12. Output, scripting, and configuration

- **Streams.** Progress goes to stderr and results to stdout: what a person needs to follow the work, `-v` adding the work behind the scenes, and `-vv` every step. `--json` writes one versioned envelope, `{"version", "command", "exit_code", "error", "result"}`, with snake_case keys. A command's result has one shape wherever it stops: what it didn't reach is left out or empty, the steps it goes on to are inside it, and the exit code says where it stopped (revised 2026-09-27, b4dfbb1b). JSON never implies approval of a change.
- **No terminal, no prompts.** A missing choice is an error naming the argument that supplies it.
- **Exit codes, as today.**
  - 0: the requested outcome, or for `--enqueue`, the request saved.
  - 1: errors and refusals.
  - 2: a failed check.
  - 3: attention needed.
  - 130: an interrupt.

  The result data keeps individual target results; the exit code isn't the whole report.
- **Identifiers.** Run IDs (`check-42`) and checkpoint names (`tidy-3`) appear where people use them. Digests, record IDs, and lease terms appear only at `-v` and in JSON.
- **Errors** name what was asked for, what got in the way, what was kept, and one next command.
- **Configuration.** Settings live in `~/.dockhand/config.toml` (decision 15). Precedence is flags, then environment, then the file. An unknown key is refused by name. Values freeze into accepted work.

```toml
worktrees = "~/Source/macports-branches"
maintainer = "{example.org:ada @ada} openmaintainer"

[build]
default = "host"          # the build profile a check takes: host, minmax, macports-ci, all, or yours
unattended = "minmax"     # the profile of a check nobody looks over: serve's and bump's

[check]
tests = "declared"        # declared | required | skip
baseline = false

[submit]
rerequest_review = "ask"

[serve]
ship_to = "list"          # list | commit | check | submit: how far serve carries your outdated ports (§11)
outdated_at = "07:00"     # when it looks for new releases each day
submit_limit = 10         # at most this many PRs a day
notify = true

[providers.local]
prefix = "/opt/local"     # where --on local builds; --on local:<prefix> names another
keep = false              # leave the branch's versions active after a check, as check --keep does
clean = []                # prefixes that are dockhand's own to clean
serve = false             # let serve check there, under a sudo rule with no password
```

## 13. Promises

- Nothing is pushed to `macports/macports-ports`. Pushes go to your fork, or to a contributor's PR branch after checking that GitHub allows it.
- Every push to an existing branch is conditional on the head last observed there. An intervening push stops with a comparison.
- No publication happens without a person's decision: `submit` binding an exact revision, `ship` or `bump`, where running the command is the consent and the holds guard the path, or `serve.ship_to = "submit"` (or `--ship-to submit`), which the person turned on and which is limited to the branches `serve` prepared that passed with nothing held.
- `tidy` never changes the final tree beyond the lint fixes `check` makes too (§8). Every rewrite keeps a checkpoint that `undo` can bring back.
- A result names its snapshot or commit, targets, providers, and releases. An old result never reads as current.
- A failed substantive target is never published except as a draft, with `--no-check`, which the PR discloses, or accepted by the person where a baseline fails it the same way, which the PR names.
- A human-edited PR title and description are kept.
- Accepted work is durable. Closing a terminal loses nothing, and whether anything is running it is always stated.
- `--plan` changes nothing, and says what it had to fetch to find out.

## 14. What v3 changes in the direction record

v3 retired the direction record's changeset vocabulary and its continuation rules (decisions 19 to 27, and 37's branch names): a port name never selects a branch, authoring commands never commit, `tidy` makes the commits by §8's rules, and "changeset" became **branch**. The decisions that stand are in [decisions](decisions.md).

## 15. Where v3 pushes back

1. **Codex's `submit --allow-incomplete` isn't adopted for substantive targets.** A ready-for-review PR whose own update fails to build spends a reviewer's time on something the author already knows is broken. §3's publication rule stays: revision-only targets and extras can be accepted, as can a failure the baseline shows at the base too, and any other failing substantive change goes out as a draft.
2. **`update.then`, from the earlier draft, is dropped.** A config key that makes `update` publish on some machines breaks "each command means the same thing everywhere". `ship` and `bump` are explicit, cheap, and enough for the maintainer who wants passing work to go straight to a PR.
3. **Serve's submission is opt-in and guarded.** It is off by default, and it can be a flag (`--ship-to submit`) or a config key (`serve.ship_to = "submit"`), the person's decision. The guardrails keep what the morning look was for: anything the upstream comparison flags (a license change, new dependencies, changed build files) waits for a person, since a passing build can't catch those. Because a config key carries the authority to every future `serve`, `serve` announces it every time it starts.
4. **`create`, not `new`, for a new port.** With `start` creating branches, `new` would read as "new branch" as easily as "new port".
5. **`review` is its own verb.** Posting on someone else's PR is the one outward act that isn't about your own branch. It shouldn't hide behind a flag on `check`.
6. **No socket service yet.** On one Mac, sessions plus a journal in the database give observers everything the socket would, with no protocol to version and no daemon lifecycle to get wrong. Revisit if journal polling shows up in measurements, or if something that isn't a dockhand process needs to submit work.
7. **Sparse worktrees stay the default**, though Codex proposed full ones. Ten full checkouts of a 40,000-port tree are gigabytes. A sparse one grows as you `edit` another port. And the evaluator materializes anything outside the sparse set that it reads.
8. **The branch record has an identity separate from the ref name.** "Branch" is the only word on screen, but a Git rename, or a PR head that must never move, would break an identity that is just a name. The ID stays behind `-v`.

## 16. From today's commands

v2's verbs map onto v3's with no aliases: `verify` became `check`, `publish` `submit`, `abandon` `archive`, `gc` `clean`, and `bump-revision` `revbump`. v2 is at tag `v2-final`.

## 17. How to judge it before building it

The engine's acceptance cases, from decision 44, stand: `--only` still delivers the candidate library; a library amend with unchanged coordinates allows no reuse; an added optional file invalidates reuse; a revision bump with a hook edit is substantive; and a guest lost during its third target keeps the first two results. The rest of v3's judgment is the acceptance harness's (tools/acceptance).

