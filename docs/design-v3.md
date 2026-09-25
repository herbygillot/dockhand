# Dockhand Design v3

Accepted direction, 2026-09-25. Nothing in it is implemented yet. [The roadmap](roadmap.md) orders the work.

## Sources and status

Design v3 combines three things:

- **The spine** is the Codex proposal, [a terminal workspace for MacPorts contributions](reviews/2026-09-24-terminal-experience-reimagining.md). This covers context from the worktree, authoring that doesn't commit, checking unfinished work, tidying by logical change, and explicit submission.
- **Additions** come from the [earlier terminal-experience draft](reimagining/terminal-experience.md): MacPorts' contribution specifics, `create` from a URL, `submit --passing`, and the needs-your-attention dashboard.
- **The engine contract** comes from the [direction record](reviews/2026-09-23-contracts-direction.md), decisions 19–44. It is restated here in the new vocabulary. Where v3 changes a decision, [§14](#14-what-v3-changes-in-the-direction-record) says so.

It also settles the [instance coordination](instance-coordination.md) design ([§11](#11-serve-the-queue-and-instance-coordination)).

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
7. **Publishing is always a person's decision.** Either the decision is about an exact revision (`submit`, `submit --check`, `submit --passing`), or it's a standing one the person gave `serve` when starting it (`serve --submit-passing`). A passing check never publishes on its own authority.
8. **Speak MacPorts.** Commit subjects, the PR body, and the checks follow the MacPorts guide, the commit-message wiki, the PR template, and CI's actual behavior.
9. **Every refusal carries the way forward.** It names the obstacle, what was kept, and one concrete next command.
10. **Git works without dockhand.** Dockhand's records enrich branches rather than owning them. Both workspace styles, managed worktrees and your own checkout, have the same capabilities.

## 3. The model

This section is the consolidated contract. The code's `record.Change` becomes the branch record. Existing contributions migrate as one-directory branches, and nothing is discarded.

### The nouns

| Noun | What it is |
| --- | --- |
| **branch** | Stable identity (an ID independent of the ref name), the Git branch that locates it, its base, title, lifecycle, and at most one PR. |
| **revision** | An immutable candidate: a source tree and base. Either a **commit** (committed source) or a **snapshot** (captured working files, numbered per branch). The changed scope derives from it. |
| **verification plan** | For one revision: the frozen coverage, exclusions with their reasons, prerequisites, and environment choices. |
| **provider** | A way of building: `tart` (a clean VM per release), `prefix` (a MacPorts installation on this Mac), `github` (MacPorts' CI workflow in your fork), or `command` (your own script). |
| **run** | One accepted check request: a plan, and the providers and releases it asked for. It has an ID that people type (`check-42`). |
| **guest execution** | For one run, provider, and release: the single owner of that environment. It holds per-target checkpoints. |
| **target result** | What happened to one target, in one configuration, with particular inputs. It is reusable only while those inputs still apply. |

On screen, a person needs only branch, port, check, run, provider, and PR. The others appear with `-v` and in JSON.

### Scope, targets, and the plan

- **Derived scope.** This follows MacPorts CI's rule. A change to a `Portfile` or under `files/` marks its directory (`git diff --name-only master...`). The targets are every subport of each changed directory, less those `replaced_by`, `known_fail`, or unsupported on the platform, in dependency order. Commits never enter into it.
- **Target kinds.** A target is **revision-only** only when its Portfile diff touches nothing but `revision` declarations (shared code included) and nothing under `files/` changed. This is proven from the source, never from evaluated metadata alone. Anything unknown counts as substantive.
- **The plan keeps four things apart:**
  - the changed scope;
  - CI eligibility per platform, with the reason for every exclusion;
  - the selected coverage (`--only`, `--also`);
  - the prerequisites that coverage needs.

  A changed prerequisite is always included, so `--only appB` still builds the changed `libA` it depends on, and `appB` is blocked if `libA` fails. An old binary is never substituted. A target whose evaluation fails leaves the plan unresolved; it is never dropped as ineligible.
- **Execution.** Each provider and release gets one guest execution. It follows MacPorts CI's order: lint every target, then for each target in dependency order, activate exactly its dependencies, fetch, checksum, install, and test (advisory). Each target's result is checkpointed as it finishes. A target whose changed dependency failed is **blocked**, not failed. Infrastructure failures are retried; verdicts never are (decision 30).
- **Evidence.** A result is reused per target when every recorded input still matches: the observations evaluation made, negative ones included, and the digest of every archive it consumed (decisions 28 and 44). Until that lands, reuse is whole-tree.
- **Publication rule.** Every selected substantive target must pass. A failed revision-only target, or a failed extra from `--also`, can be acknowledged with `--accept <port>`, which is recorded and shown in the PR as "cause not established". A baseline is evidence and never unblocks publication by itself.
- **Identity and names.** A branch dockhand creates is named `dockhand/<name>`, where `start <name>` supplies the name. Without one, it's `dockhand/<first port>-<short ID>`, per decision 37. The name is never changed once a PR exists. A branch renamed with Git keeps its identity through `adopt`'s reconciliation.
- **Titles.** A `--title` given at any step is kept. With one substantive port and no title, the title is that port's commit subject. With several and no title, `submit` asks. Dockhand never composes one, and never retitles an existing PR unless `--title` is passed.

## 4. Context and selection

The context is the branch you're working on. It comes from, in order:

1. An explicit `--branch <name>`. The `dockhand/` prefix is optional.
2. The worktree you're in, and the branch checked out there.
3. Otherwise, a command that changes something asks on a terminal and refuses with the choices in a script.

The first line of every command that changes something names the branch and directory. There is no persistent "current branch" shared between terminals.

**A port name never picks a branch.** Two branches may both touch `jq`, and neither silently receives the other's next edit. `dockhand status --port jq` finds them. `--new` starts a branch for an authoring command: `dockhand update jq --new` creates `dockhand/jq-4k2p` and updates jq in it.

At the repository root, with no context, a terminal offers the choice, with the obvious one first:

```console
$ dockhand update jq
jq is in no open branch.
? start dockhand/jq-4k2p for it? [Y/n] y
```

```console
$ dockhand update jq
jq is changed in 1 open branch: dockhand/jq-update (#34901, your update to 1.8.0)
? update jq there, or start a new branch? [t]here / [n]ew / [q]uit
```

A script gets an error that names `--branch jq-update` and `--new`.

### Where branches live

- **Managed worktrees.** `dockhand start <name>` creates a branch from freshly fetched `master` in a **sparse worktree**, a directory beside your clone by default (`~/src/macports-branches/<name>`, set by `worktrees` in the config). A sparse worktree holds `_resources` plus the directories of the ports the branch touches, and grows when you `edit` another port. It's a normal Git worktree that any editor can open. `dockhand path <name>` prints its path. Dockhand can't change the parent shell's directory and doesn't pretend to.
- **Your own checkout.** `dockhand start <name> --here` creates the branch in the checkout you're in. `dockhand adopt` tracks the branch you're already on, as it is. Adopting never moves or rewrites anything. `start --here` refuses to displace uncommitted work, and says how to keep it.

Both styles get the same editing, checking, tidying, and submitting.

## 5. Commands

Help teaches the short loop first.

| Purpose | Commands |
| --- | --- |
| Start or enter work | `init`, `start`, `adopt`, `path` |
| Author | `update`, `checksums`, `revbump`, `create`, `edit` |
| Understand | `status`, `diff`, `impact`, `outdated`, `info` |
| Check | `check`, `logs`, `retry` |
| Prepare for review | `tidy`, `submit`, `rebase` |
| Others' work | `adopt --pr`, `review` |
| Keep work moving | `queue`, `wait`, `cancel`, `watch`, `serve` |
| Occasional | `providers`, `auth`, `restore`, `archive`, `clean`, `config`, `db`, `explain` |

```console
$ dockhand help
dockhand: author, check, and submit changes to MacPorts ports

The loop
  start     start a branch from fresh master (--here: in this checkout)
  update    update a port to a newer release
  edit      open a port's files in your editor
  check     build and test what you have, committed or not
  tidy      shape the branch's commits for review
  submit    open or update the pull request

Understand   status · diff · impact · outdated · info
Check        logs · retry
Review       rebase · adopt --pr · review
Queue        queue · wait · cancel · watch · serve
Occasional   init · providers · auth · restore · archive · clean · config · db · explain

dockhand help <command> for details · dockhand help journeys for walk-throughs
```

Every authoring command accepts `--plan`, which shows the intended edit and changes nothing on your branch, in your files, or on GitHub. A plan does say what it had to fetch or evaluate to find out.

## 6. Journeys

These are mock sessions. Port names are real. Versions, PR numbers, and durations are illustrative, and `libharbor` is fictional.

### 6.1 First run

```console
$ cd ~/src/macports-ports
$ dockhand init
Using this ports repository; upstream is macports/macports-ports.

  Authoring    ✓ ready (MacPorts 2.12.6 at /opt/local)
  Your ports   ✓ 212 ports list @ada as a maintainer
  Branches     ? keep worktrees beside this clone, in ~/src/macports-branches? [Y/n] y
  Providers    tart    · not set up: dockhand providers setup tart   (macOS 26, ≈ 30 min, 45 GB)
               github  · needs a GitHub login and your fork's Actions enabled
               prefix  · optional: dockhand providers setup prefix
  Publishing   · not set up: dockhand auth login, when you're ready to submit

Next: dockhand start <name>, or dockhand outdated --mine
```

`init` doesn't require a GitHub login, a fork, or a provider before local work can start. It reports each capability separately: authoring, checking, and publishing. It asks for more only when something needs it, and names each cost before incurring it: an image download, an index build, a login.

### 6.2 The ordinary update

```console
$ dockhand start jq-update
Created dockhand/jq-update from master 4c1e2d0 (fetched just now)
Directory: ~/src/macports-branches/jq-update
Next: cd "$(dockhand path jq-update)"

$ cd "$(dockhand path jq-update)"
$ dockhand update jq
jq-update · ~/src/macports-branches/jq-update
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
Next: dockhand tidy

$ dockhand tidy
Proposed commit
  jq: update to 1.8.1
Includes textproc/jq/Portfile
Review diff [d] · Edit message [e] · Apply [a] · Cancel [q]
> a
Created 1 commit (checkpoint tidy-1). The files are unchanged, so the check still applies.

$ dockhand submit
jq-update · ready to submit
  Title    jq: update to 1.8.1
  From     ada/macports-ports:dockhand/jq-update
  To       macports/macports-ports:master
  Commits  1, follows MacPorts' commit rules
  Checks   passed on tart macOS 26 arm64 for this commit's tree
  Other PRs  none open for jq
Preview [p] · Edit description [e] · Submit [s] · Cancel [q]
> s
Opened #34901  https://github.com/macports/macports-ports/pull/34901
```

The same thing as one explicit line, for someone who knows what they want:

```sh
dockhand update jq --new --submit
```

`--submit` means tidy the new branch's single authoring edit, check it, and submit that exact commit once the check passes. It is the combination of three commands, spelled out, and it previews all three. Without a terminal, the tidy and submit previews are skipped only when the plan is unambiguous (§8), and `--yes` never resolves an ambiguous plan. A plain `submit` never tidies or checks by itself.

### 6.3 When dockhand can't edit the Portfile

```console
$ dockhand update openjdk21
openjdk21-update · ~/src/macports-branches/openjdk21-update
✗ can't update openjdk21 by itself [hook-exec]
  java/openjdk21/Portfile:48: its pre-fetch hook runs `exec`, and dockhand won't guess what that changes.
  Kept: the branch, unchanged.
  Edit the version yourself; `dockhand checksums openjdk21` then fills in the rest:
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

`create` takes a forge URL, a registry name (`pypi:`, `crates:`, `go:`), or a plain name. `--like <port>` starts from the conventions of an existing port instead of a template. It fills in what it can observe and marks what it guessed, since it never presents a guessed license or maintainer as fact. The maintainer line comes from your config (`{@ada example.org:ada} openmaintainer`). The new files are added to the branch's index, so a first check includes them. The file follows `port lint --nitpick`'s layout: the modeline, four-space columns, and `rmd160 sha256 size` checksums. Because the guide asks new submissions to fix `--nitpick` findings, `check` treats them as errors for a new port.

A handwritten Portfile is just as much a first-class path. `check` builds it the same way whether or not `create` made it.

### 6.5 Checksums and a stealth update

```console
$ dockhand checksums croc --new
croc-checksums · ~/src/macports-branches/croc-checksums
croc 10.2.4 · the distfile changed upstream without a new name (stealth update)
  was   sha256 1f3a…c2d9   size 7,114,391
  now   sha256 9b0c…77e1   size 7,114,508
  inside the archive: 1 file differs, go.sum (+2 −2)   (dockhand diff --archive for all of it)
Updated checksums and dist_subdir croc/10.2.4_1, so mirrors keep both archives.
Inspect the source change before deciding whether it needs a revision bump.
```

The `dist_subdir` recipe is the MacPorts guide's. Dockhand calls out the stealth update and shows what changed inside the archive, but leaves the question of a revision bump to you.

### 6.6 Revision bumps

The reason is the subject, because that's how maintainers write it: 91 of 93 recent upstream rebuilds put the reason in the subject.

```console
$ dockhand revbump gdal inkscape --subject "rebuild for poppler 25.09.0" --branch poppler-25.09
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
Inspect: dockhand logs check-42 --port harbor-viewer
```

There is no primary port with secondary ports attached. Revision-bumped ports are changed ports like any other, and `--also` adds unchanged ports as extra coverage. `--revbump-dependents` bumps direct library dependents only, taken from the index, and `--plan` shows the list first. `--except <port>` takes a port out of either list.

### 6.8 A failed check

```console
$ dockhand logs check-42 --port harbor-viewer --errors
src/render.c:212: error: too few arguments to function 'harbor_open'

$ dockhand check --baseline --only harbor-viewer
harbor-viewer at master 4c1e2d0 (libharbor 1.9) · tart macOS 26 arm64
  ✓ builds at the base. This branch's result differs; the cause isn't established.
```

A baseline reports what happened in each run and nothing more. It is opt-in: `--baseline`, or `check.baseline = true`. You fix the failure, then check again:

```console
$ dockhand edit harbor-viewer          # add files/patch-libharbor-2.diff, list it in patchfiles
$ dockhand check
libharbor-2 · captured working files as snapshot 5
  ✓ reused libharbor, harbor-cli, harbor-tools: their inputs are unchanged
  harbor-viewer   ✓   3m40s
Passed for snapshot 5.
```

Only `harbor-viewer` is rebuilt, because the other results still apply. Until per-target reuse lands, the whole plan is rebuilt instead, and the output says so. `retry check-42` repeats the pinned request exactly; `check` captures newer edits.

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
Also noticed, for you to fix (tidy never changes files):
  ✗ revision is 1 after a version update; MacPorts expects 0

Review patches [d] · Change groups [g] · Edit messages [e] · Apply [a] · Keep history [k] · Cancel [q]
> a
Created 1 commit. Checkpoint tidy-3 keeps the old history (dockhand restore tidy-3).
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
Directory: ~/src/macports-branches/pr-34905
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
  lint                     ✓ port lint
? post as: c comment · r request changes · e edit first · n don't post  r
✓ review posted on #34905: changes requested, 1 inline comment
```

`review` evaluates, lints, and applies §8's commit rules. `--check` also builds the PR. It posts nothing without showing the text and asking. "Request changes" is offered only when your account has write or triage access to macports/macports-ports, and dockhand says why when it isn't offered. `--markdown` prints the text for pasting. When you review a PR again after it changes, dockhand says which earlier findings are resolved instead of repeating them.

### 6.12 Working through your ports

This journey is the core of the maintainer's day.

```console
$ dockhand outdated --mine
  PORT        NOW       NEWEST    DOCKHAND CAN
  croc        10.2.4    10.2.5    update
  fd          10.2.0    10.3.0    update
  jq          1.7.1     1.8.1     update
  xan         0.52.0    0.53.0    update (cargo2port)
  openjdk21   21.0.8    21.0.9    edit by hand (dockhand info openjdk21 says why)
5 of 212 ports have newer releases · 3 couldn't be checked (-v says why)

$ dockhand update --outdated --mine --check
Will start 4 branches, one per port (unrelated ports go in separate PRs):
  dockhand/croc-7hq2 · dockhand/fd-2mxa · dockhand/jq-4k2p · dockhand/xan-9ct1
Skipped: openjdk21 (needs a hand edit)
? go ahead? [Y/n] y
  ✓ 4 branches updated and tidied into one commit each; 4 checks queued
  serve isn't running: dockhand serve, or dockhand wait to run them here
```

Before creating anything, the batch shows how it will split the work. The server works through the queue. When it's done, the passing branches wait for one quick look each:

```console
$ dockhand submit --passing
3 branches passed their checks; 1 didn't (dockhand status --attention)
  y submit · n not now · d diff · r release notes

croc-7hq2   croc 10.2.4 → 10.2.5 · tart:tahoe ✓ · Portfile +3 −3
            upstream: go.mod adds golang.org/x/net v0.44.0
? submit? y     ✓ opened #34902
fd-2mxa     fd 10.2.0 → 10.3.0 · tart:tahoe ✓ · Portfile +3 −3
            ! upstream's LICENSE file changed; the Portfile's license line may need to follow
? submit? n     · kept for later: dockhand status fd-2mxa
jq-4k2p     jq 1.7.1 → 1.8.1 · tart:tahoe ✓ · Portfile +3 −4
? submit? y     ✓ opened #34901
```

`submit --passing` lists each branch whose check passed for exactly what would be submitted: a committed tree, with no edits left out. It shows each one's submission preview in brief. The `!` lines compare the old and new upstream archives, which dockhand already fetched for the checksums, looking for changed license files, build files, and declared dependencies. Those are what a reviewer would ask about, and what a passing build can't catch.

`serve` can do the morning's preparation by itself with `serve.updates = "check"`. It then finds new releases of your ports, creates and tidies a branch for each, and checks them, so `submit --passing` is all that's left. By default it never submits. `serve --submit-passing` goes one step further ([§11](#11-serve-the-queue-and-instance-coordination)).

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
- `--also <port>` builds unchanged ports against the branch. `--also-dependents` does the same for the changed ports' direct dependents.
- `--variants` applies to a single selected target. Mixed per-port variants need a saved coverage profile.

A narrowed check never quietly shrinks what `submit` requires.

`--on <provider>[:<releases>]` selects where the build runs. Examples are `--on tart:sonoma,tahoe`, `--on tart:all`, `--on prefix`, and `--on github`. A bare release name means Tart, and `check.on` in the config sets the default. Several `--on` flags mean all of them must pass, never one of them (decisions 14 and 29). Switching a queued or running check to another provider is explicit: `check --on github --replace`.

| Provider | Builds in | Clean each time | What its result can claim |
| --- | --- | --- | --- |
| `tart` | a fresh clone of a prepared VM image per release | yes | CI's order, declared tests, any release with an image |
| `prefix` | a MacPorts installation on this Mac | no; says how many ports were already installed | quick iteration on this Mac's release |
| `github` | MacPorts' CI workflow in your fork | yes | only what CI does: default variants, `port lint` without `--nitpick`, the built-in test phase, the runners' releases |
| `command` | whatever your script does | as it reports | labelled "reported by <name>"; dockhand can't vouch for it |

The default `prefix` is a separate MacPorts installation that you own, set up by `dockhand providers setup prefix` without root. It needs no `sudo` and leaves your everyday ports alone. MacPorts' binary archives only serve `/opt/local`, so its first build compiles every dependency from source.

Your everyday `/opt/local` can also be a provider, but only as an explicit opt-in. It installs the branch's versions over your own and needs root. Dockhand restores the previously active versions after the build, and says so when it can't.

A `command` provider receives a request file (the revision as a Git bundle, the targets in order, the release, the variants, and the test policy) and writes a result file with each target's phases. Configuring any provider that pushes somewhere shows that push when the check is accepted: "only checking" never hides a write to your fork.

### Results

`check` covers lint, fetch and checksum, build and install, and declared tests. `check --lint` is the cheap partial form. Each target reports one of passed, failed at a phase, blocked by a failed changed dependency, not run, or could not evaluate. "Could not evaluate" never becomes "not applicable".

Tests are advisory, as in MacPorts CI: "build passed; tests failed (advisory)" is shown as exactly that. `--tests required` makes a test failure decisive.

Independent targets continue past a failure. Interruptions, infrastructure trouble, and a failed build are distinct outcomes. The result says which snapshot or commit it covers, and `status` says when the current work has moved on: "passed for snapshot 3; the files have changed since".

## 8. Tidy, and what MacPorts asks of commits

`tidy` answers the question: **what should a reviewer see as the separate changes in this branch?**

It proposes a series of commits, built from these hints:

- what the authoring commands recorded (the kind of change, and any subject you gave);
- file paths;
- existing commit messages;
- which commits read as follow-ups to earlier ones.

Hints are not proof. Mixed commits and overlapping hunks produce a plan for you to review. Dockhand admits uncertainty rather than inventing a polished but wrong series, and history that already has a good shape passes through unchanged.

A plan is **unambiguous** when every group is one port directory's edits from a single authoring command, with a subject dockhand wrote. That's the only kind `--yes`, a script, `update --submit`, or `serve` may apply without review. Other options:

- `tidy --squash --message "…"` squashes the whole branch explicitly, saying which commits and edits it includes.
- `tidy --plan --out <file>` saves a reviewable plan, and `tidy --apply <file>` applies it. A saved plan is bound to the base, the tip, and the captured working state, and goes stale if any of them change.

Tidy guarantees:

- The final tree is exactly preserved, so content evidence still applies.
- Human authorship and existing trailers are kept. Combining commits by several authors asks you to choose the attribution.
- Before applying, it saves a named checkpoint of the old refs and any captured index or working state. `dockhand restore <checkpoint>` restores it, after checking for newer work.
- Merges are never flattened without a specific, reviewed choice.

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
| Lint | ✗ `port lint` errors, which is what CI runs; ! `--nitpick` findings, which count as ✗ for a new port |
| Shared files | ! a PortGroup changed; lists who loads it. CI builds nothing for `_resources` alone |
| Other PRs | ! an open PR changes the same port |
| Maintainer | · you aren't the maintainer: shows whether the port is `openmaintainer`, and that others merge only after 72 hours |

Subjects dockhand writes:

| Change | Subject |
| --- | --- |
| New port | `rift: new port` |
| Update | `jq: update to 1.8.1` |
| Rebuild | `gdal: rebuild for poppler 25.09.0` (your `--subject`; never invented) |
| Checksums | `croc: update checksums (stealth update)` (the reason in parentheses is asked for) |
| Obsolete | `foo: replaced by bar` |

Trailers go last: `Closes:`, then `See:`, then dockhand's `Generated-By:`.

## 9. Submit

`submit` opens or updates the branch's one PR. Its preview shows the commits, the head and base repositories, the title, the description, the coverage, the commit-rule findings, and any other open PRs for the same ports.

- **`submit`** needs committed source and results that apply to its tree, under the publication rule in §3. If the worktree has edits, it lists what would be left out and asks for `--head` or `--tidy`. Evidence from the working tree never authorizes pushing a different committed tree.
- **`submit --draft`** allows unfinished or failing checks, and shows them in a draft PR, which gets MacPorts CI started early. The source must still be committed. `submit --ready` takes the PR out of draft.
- **`submit --accept <port>`** acknowledges a failed revision-only target, or a failed extra from `--also`, for this exact revision. It's recorded, and shown in the PR as "cause not established". A failed substantive target cannot be accepted: the way to share one is `--draft`.
- **`submit --no-check`** publishes without any check. The PR says no local build ran, and MacPorts CI becomes its only check.
- **`submit --passing`** is the batch review from §6.12.
- **`submit --check`** submits this exact commit once its check passes. It's a single binding of source, destination, and content, made by you, and it goes stale if the branch changes.

The PR body is written in the template's own sections, and ticks only what dockhand has evidence for.

- **`#### Description`**: for one commit, its body. For several, a table of commit, port, and change.
- **`###### Type(s)`**: `bugfix`, `enhancement`, or `security fix`, ticked when stated (`--type`), or when a commit cites a CVE. MacPorts' bot detects "update" from a title containing ": update", and "submission" from a new Portfile, by itself.
- **`###### Tested on`**: the template's lines for each release built (`macOS 26.1 25B78 arm64`, then Xcode or the Command Line Tools version). Each names the provider and what its result establishes: "built in a clean VM", "built in a MacPorts prefix with 143 ports already installed", "MacPorts CI workflow in ada/macports-ports" with the run's link, or "reported by <name>". A grid below shows each port against each provider and release, including anything accepted, baselined, or not built locally.
- **`###### Verification`**:
  - `port lint`, `sudo port test`, and install are ticked from recorded results. Dockhand builds from source as CI does, without trace mode (decision 28), so the `-vst` item is ticked with that stated, never silently.
  - "Commit Message Guidelines" and "squashed and minimized" are ticked when the commit rules passed on what's being submitted.
  - "Other open pull requests" is ticked when dockhand searched and found none; otherwise the findings are linked.
  - "Tested basic functionality" and "most important variants" are asked, and a yes is recorded as your statement. `--variants each` evidence ticks the variants item by itself.
  - Items that don't apply are removed, as the template's comment asks.

`--skip-notification` adds `[skip notification]`, which stops MacPorts' bot from mentioning maintainers. `submit` suggests it for a batch of trivial rebuilds.

A description a person has edited is kept. Only the span dockhand owns, from Tested on through Verification, is regenerated, and only while the text there is still what dockhand wrote.

## 10. Status and the attention list

With no arguments, `dockhand` shows status. Inside a branch, that's the branch's own status. At the repository root, it starts with what needs you:

```console
$ dockhand
Needs you
  ! jq-update       #34901 changes requested by @ryandesign (2h)        dockhand edit jq
  ✗ libharbor-2     harbor-viewer failed to build (check-42)              dockhand logs check-42 --port harbor-viewer
  · fd-2mxa         passed; waiting for you to submit                     dockhand submit --passing
  ! rift            snapshot 2 passed; files changed since                dockhand check --branch rift

BRANCH          PORTS  WORK                CHECKS                    PR
jq-update           1  1 commit            passed for this commit    #34901 changes requested
libharbor-2         3  edits after check   1 failed, 3 passed        —
croc-7hq2           1  1 commit            passed                    #34902 open, CI ✓
gdal-rebuild        1  1 commit            passed                    #34880 maintainer timeout ends in 41h
rift                1  draft files         passed for older work     —

serve: running · tart 1 of 2 VMs busy · queue: 2 runs
```

- **`status --attention`** prints only the "needs you" list, for a prompt or a script. Each row ends with the one command that moves it forward.
- **`status --port jq`** finds every branch touching jq.
- **`status --all`** includes merged and archived branches.
- **`status <branch>`** shows one branch: its commits, targets and their results by provider and release, the plan's exclusions, and the PR.

Each column answers one question. No single "done" field encodes source state, checks, and review together. Observed forge state shows its age.

`dockhand watch` is the same view live, with keys for check, logs, tidy, and submit that use each command's own semantics and confirmations. It observes only: `serve` does the work, so opening the view never starts anything. Everything it shows is also available as plain output, for SSH sessions, scrollback, and screen readers.

## 11. Serve, the queue, and instance coordination

### What a person sees

```sh
dockhand serve                 # drive the queue in this terminal until interrupted
dockhand serve --install       # a launchd agent: starts now and at every login
dockhand serve --uninstall
dockhand serve --drain         # run what is queued now, then exit
dockhand serve --submit-passing   # also open PRs for the branches it prepared that pass (see below)
```

| Situation | Behavior |
| --- | --- |
| `check` while `serve` runs | Submit the request and follow it here. Ctrl-C detaches; the run continues. |
| `check` with no `serve` | Run this request in the foreground, with the same runner, and say so. Ctrl-C stops it safely and keeps finished results; `retry` resumes. |
| `check -d` / `--enqueue` with no `serve` | Save the request; print "queued, nothing is running it: dockhand serve". Exit 0 means saved, not passed. |
| `wait <run>` | Observe that exact run. With no `serve` running, run it here, as a foreground check would. |
| `serve` starts while a foreground check runs | The foreground command keeps its own run to the end; `serve` takes everything else. |
| A second `serve` | Stands by, says which process leads, and takes over when the leader dies. |
| `serve` or its Mac restarts | Reconcile active provider work before retrying anything; finished target results are kept. |

```console
$ dockhand queue
serve: running (pid 4711, up 3h) · tart 2 of 2 VMs · prefix idle · github idle

RUN        BRANCH        SOURCE       ON           STATE      DETAIL
check-42   libharbor-2   snapshot 5   tart:tahoe   running    harbor-viewer: build
check-43   jq-update     commit 7e3f  tart:tahoe   queued     waiting for a VM
check-44   croc-7hq2     commit a1b0  tart:tahoe   queued     from serve.updates
```

The queue holds immutable check requests. Editing a branch after it was queued never changes what the request means. A newer snapshot of the same branch doesn't cancel an older queued run; it's listed beside it, and `cancel <run>` is explicit. People come first: work someone is attached to, or submitted by hand, runs ahead of work `serve` started by itself. Branches otherwise take turns, and each provider has its own capacity. `queue pause` stops admissions while running checks finish, and `queue resume` restarts them.

What `serve` does besides running checks (each can be turned off, and all show in `queue`):

- **Follows PRs.** Every few minutes it reads state, reviews, and CI for your open PRs, and turns changes into attention rows.
- **Keeps `master` fresh** for `start`, and marks branches that no longer rebase cleanly.
- **Finds updates** (`serve.updates`):
  - `list`, the default: counts new releases of your ports in `status`.
  - `draft`: creates and tidies a branch for each new release.
  - `check`: also checks each one, so the morning is `submit --passing`.

  None of these submits by default.
- **Submits what passed, only when started with `--submit-passing`.** By default `serve` only builds and tests. With the flag, and with `serve.updates = "check"`, it opens a PR for each branch it prepared whose check passed. The flag applies to that `serve` process only: `serve --install --submit-passing` records it in the launchd agent, and `serve --install` without it takes it away. Guardrails:
  - **Scope.** Only the branches `serve` itself created from new releases. Your own branches are submitted by you, with `submit`, `submit --check`, or `submit --passing`.
  - **What passed.** The check must have passed on the exact committed tree being submitted, under the publication rule in §3, with no acknowledgements needed. A branch that needs `--accept` waits for a person.
  - **Held for a look.** A branch with any `!` finding from the upstream comparison, such as a changed license file, new declared dependencies, or changed build files, or any commit-rule warning, is not submitted. It lands on the attention list instead.
  - **A daily limit.** At most `serve.submit_limit` PRs a day (10 by default). The rest wait for the next day, so a release wave doesn't land on reviewers all at once.
  - **Visibility.** `queue` and `status` say "serve opens PRs for passing updates". Every PR it opens appears on the attention list as "opened by serve", and the PR body says that it was submitted without a person's review.
- **Cleans up** on decision 36's schedule.
- **Notifies** through macOS notifications, when a check finishes or a PR changes (`serve.notify`).

### How it works: sessions, leases, and a journal

This settles the [coordination note](instance-coordination.md). The domain is still one Mac, one person, a few terminals, and one database. The database stays the only coordination medium: there is no socket and no network listener.

1. **Sessions.** Every dockhand process that runs work, or that watches, records a session row: a random session ID, pid, process start time, a start token, its kind (`serve`, foreground, or observer), dockhand version, schema, and a heartbeat every few seconds, written outside long transactions. Everything runs on one Mac, so a session's death is known directly. A pid that no longer exists, or exists with a different start time, is dead at once. The heartbeat covers a hung or sleeping process, and it counts as stale only by the writer's own recorded clock, and only after several missed beats, so a laptop waking from sleep doesn't flap.
2. **Fenced leases replace anonymous claims.** Every claim on a job, run, guest execution, provider reservation, or control application names its session and carries a fencing generation. Every write checks the generation, and a stale writer's late write is refused. When a session dies, its leases are void at once, instead of expiring at the step deadline ten or fifteen minutes later. External actions keep today's idempotency and reconciliation, because a fence can refuse a stale write but can't stop a paused process from making a late provider call.
3. **One leader.** `serve` takes the leader lease for its repository registration (decision 35 keeps one repository per engine). The leader drives all queued work. A foreground command without a leader leases only its own run. A second `serve` stands by and takes the leader lease when the leader's session dies. The live `watch` view is always an observer.
4. **Controls are applied by whoever can apply them.** `cancel` records its request. If a live session holds the run's lease, that session applies the cancel at its next step. If none does, the cancelling command takes the lease and applies it itself. A cancel never sits unapplied because nobody is driving.
5. **An event journal for observers.** Every transition, provider milestone, capacity wait, retry, and problem is appended to an events table, tagged with run, branch, target, session, and time, in the same transaction as the state change it describes where there is one. Today's driver-local progress lines ("cloning image", "waiting for the guest") become events. `status`, `watch`, `wait`, and a following `check` tail the journal by sequence number, so every terminal sees the same narrative. Build logs stay files beside the database. Events are pruned with their runs.
6. **Version skew.** A session records its version and schema. `serve` refuses to lead a database whose schema is newer than it knows. An observer renders unknown event kinds generically, rather than failing. `serve --install` after an upgrade restarts the agent on the new build.
7. **Across databases.** Two database files still can't see each other's leases. Dockhand keeps its own Tart home (decision 42). Registration writes the database path under the checkout's `.git`, so a second database registering the same checkout is warned. The Tart capacity window, where two databases could each overshoot by one VM, gets a lease file under dockhand's Tart home. That file is cheap now that the home is dockhand's own.

A socket-based client and service, the note's option 1, stays unbuilt. The reason is in [§15](#15-where-v3-pushes-back).

## 12. Output, scripting, and configuration

- **Streams.** Progress goes to stderr and results to stdout. `--json` writes one versioned envelope, `{"command", "exit_code", "error", "result"}`, with snake_case keys. Progress is written as JSON Lines only when asked for, with `--events`. JSON never implies approval of a change.
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
worktrees = "~/src/macports-branches"
maintainer = "{@ada example.org:ada} openmaintainer"

[check]
on = ["tart:host"]        # all of these must pass
tests = "declared"        # declared | required | skip
baseline = false

[submit]
rerequest_review = "ask"

[serve]
updates = "list"          # list | draft | check
updates_at = "07:00"
submit_limit = 10         # used only by serve --submit-passing
notify = true

[providers.prefix]
path = "~/.dockhand/prefix"
```

## 13. Promises

- Nothing is pushed to `macports/macports-ports`. Pushes go to your fork, or to a contributor's PR branch after checking that GitHub allows it.
- Every push to an existing branch is conditional on the head last observed there. An intervening push stops with a comparison.
- No publication happens without a person's decision: `submit` binding an exact revision, `submit --check` whose binding is the command itself, or `serve --submit-passing`, started by the person and limited to the branches `serve` prepared that passed with nothing held.
- `tidy` never changes the final tree. Every rewrite keeps a checkpoint that `restore` can bring back.
- A result names its snapshot or commit, targets, providers, and releases. An old result never reads as current.
- A failed substantive target is never published except as a draft, or with `--no-check`, which the PR discloses.
- A human-edited PR title and description are kept.
- Accepted work is durable. Closing a terminal loses nothing, and whether anything is running it is always stated.
- `--plan` and `--dry-run` change nothing, and say what they had to fetch to find out.

## 14. What v3 changes in the direction record

These decisions stand as written, in the new vocabulary: 1–18, 20's execution order, 21's acknowledgement (as `submit --accept`), 23's `--only` and `--also`, 24 (titles), 27's `--revbump-dependents` and `--except`, 28–33, 35–43, and 44.

Changed by v3:

| Decision | Change |
| --- | --- |
| 19, 22 (selection), 25, 26 | A port name never selects a branch. Context comes from the worktree, `--branch`, or a prompt; `--new` starts a branch. The continuation rules by verb are retired: on a terminal the prompt offers your open update of the port first; a script names `--branch` or `--new`. |
| 22 (preparation writes commits), 27 (`drop` amends) | Authoring commands edit files and never commit. `tidy` makes the commits, and the only plans applied unattended are unambiguous ones (§8). |
| 20 (one commit per directory, already superseded by 22) | The commit rules are §8's, which follow MacPorts' "one commit per logical change". |
| 29 (`verify --replace`) | Becomes `check --replace`. No fallback between providers, unchanged. |
| 21 (`publish --accept-failure`) | Becomes `submit --accept <port>`. |
| 37 (`--branch-name`) | `start <name>` names the branch. Unnamed branches keep decision 37's `dockhand/<first port>-<ID>`. |
| Vocabulary of 44 | "Changeset" becomes **branch**. A revision is a commit or a **snapshot**. **Run** is added. |
| The roadmap's "review and unattended-publication authority" | Settled: `review` posts only on confirmation; `serve` publishes only when started with `--submit-passing`, within §11's guardrails; `submit --check` and `--passing` are a person's binding. |

## 15. Where v3 pushes back

1. **Codex's `submit --allow-incomplete` isn't adopted for substantive targets.** A ready-for-review PR whose own update fails to build spends a reviewer's time on something the author already knows is broken. Decision 21's line stays: revision-only targets and extras can be accepted, and a failing substantive change goes out as a draft.
2. **`update.then`, from the earlier draft, is dropped.** A config key that makes `update` publish on some machines breaks "each command means the same thing everywhere". `--submit` is explicit, cheap, and enough for the maintainer who wants passing work to go straight to a PR.
3. **`serve --submit-passing` is opt-in and guarded, not a setting.** A standing authority to publish belongs to the process the person started, where they can see it and stop it. A config key would quietly carry that authority to every future `serve`. The guardrails keep what the morning look was for: anything the upstream comparison flags (a license change, new dependencies, changed build files) waits for a person, because a passing build can't catch those.
4. **`create`, not `new`, for a new port.** With `start` creating branches, `new` would read as "new branch" as easily as "new port".
5. **`review` is its own verb.** Posting on someone else's PR is the one outward act that isn't about your own branch. It shouldn't hide behind a flag on `check`.
6. **No socket service yet.** On one Mac, sessions plus a journal in the database give observers everything the socket would, with no protocol to version and no daemon lifecycle to get wrong. Revisit if journal polling shows up in measurements, or if something that isn't a dockhand process needs to submit work.
7. **Sparse worktrees stay the default**, though Codex proposed full ones. Ten full checkouts of a 40,000-port tree are gigabytes. A sparse one grows as you `edit` another port. And since decision 35, the evaluator materializes anything outside the sparse set that it reads.
8. **The branch record has an identity separate from the ref name.** "Branch" is the only word on screen, but a Git rename, or a PR head that must never move, would break an identity that is just a name. The ID stays behind `-v`.

## 16. From today's commands

| Today | v3 |
| --- | --- |
| `bump jq` | `update jq --new --submit`, or `start`, `update`, `check`, `tidy`, `submit` |
| `bump-revision jq --subject …` | `revbump jq --subject …` |
| `checksums jq` | `checksums jq` |
| `bump --revbump-dependents` (decided) | `update --revbump-dependents` |
| `verify` | `check` (`--on` replaces `--provider`, `--image`, and `--os`) |
| `verify --working-tree` | `check`, whose default in a worktree is the working files |
| `publish` | `submit` |
| `adopt <branch>` | `adopt` (the current branch), or `adopt <branch>` |
| `adopt --pr` | `adopt --pr` |
| `amend`, `amend --squash`, `adopt --squash` | edit, then `tidy`, or `tidy --squash` |
| `rebase` | `rebase` |
| `abandon` | `archive` |
| `sync` | `status --refresh`; `serve` follows PRs |
| `status`, `console` | `status`, `status --attention`, `watch` |
| `wait`, `cancel` | `wait <run>`, `cancel <run>` |
| `serve`, `serve --install-launchd` | `serve`, `serve --install` |
| `outdated`, `assess` | `outdated`, `info` |
| `setup` | `init`, `providers setup tart` |
| `reassociate` | `adopt` reconciles a renamed branch |
| `gc`, `db` | `clean`, `db` |

There are no aliases for the old verbs. The tool is prerelease, and the README says scripts should expect changes.

## 17. How to judge it before building it

Walk through these tasks with terminal prototypes, and ask contributors to predict each command's effect before running it:

- A first-time contributor writes a new port by hand and checks it before its first commit.
- A maintainer runs `update --outdated --mine --check`, then `submit --passing`.
- Two terminals edit separate branches that both touch jq, with no ambiguity.
- A library update and two consumers share a branch, and one consumer fails.
- Six correction commits become one; two distinct changes to one Portfile stay two.
- Work changes after a check is queued; the older result never reads as current.
- A check is interrupted with and without `serve`, and the person can say what is still running and how to resume.
- A maintainer pushes to a PR while its author tidies; the author's next `submit` keeps that work.
- A PortGroup-only branch chooses representative coverage, with `--also`, and reports its limits.
- `serve` dies mid-build; a standby or the next `serve` takes over at once, without waiting out a deadline.

The acceptance cases from decision 44 stand for the engine:

- `--only` still delivers the candidate library.
- A library amend with unchanged coordinates allows no reuse.
- An added optional file invalidates reuse.
- A revision bump with a hook edit is substantive.
- A guest lost during its third target keeps the first two results.
- A migrated one-port contribution doesn't claim coverage of newly derived siblings.
