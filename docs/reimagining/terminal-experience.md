# Dockhand, reimagined: the terminal experience

A proposal, written 2026-09-24. None of it is implemented. It asks what dockhand would feel like if it had been designed around **changesets** from its first command, rather than around a single port. It starts from the [contracts review](../reviews/2026-09-23-contracts-review.md), which found that "a contribution is one port directory and one commit" (§3.1) is the largest capability the current design excludes. Today's commands are used only as evidence: what worked, what confused people, and what the exercises taught ([messaging scan](../reviews/2026-09-19-messaging-scan.md), [deno exercise](../reviews/2026-09-16-new-user-deno-exercise.md), [output design](../output.md)). They are not a constraint.

It is written for one maintainer on one Mac, keeping many ports current. Decisions made in review are recorded in [§11](#11-decisions-and-open-questions).

This page covers the experience only: what a contributor types, sees, and can rely on. What it asks of the engine is summarized in [§9](#9-what-this-asks-of-the-engine) and left for a design of its own.

## Contents

1. [The idea on one screen](#1-the-idea-on-one-screen)
2. [Principles for the experience](#2-principles-for-the-experience)
3. [The nouns](#3-the-nouns)
4. [The command map](#4-the-command-map)
5. [Journeys](#5-journeys)
6. [Seeing everything: status and watch](#6-seeing-everything-status-and-watch)
7. [The queue and the server](#7-the-queue-and-the-server)
8. [Reference](#8-reference)
9. [What this asks of the engine](#9-what-this-asks-of-the-engine)
10. [From today's commands](#10-from-todays-commands)
11. [Decisions and open questions](#11-decisions-and-open-questions)

## 1. The idea on one screen

A **changeset** is a branch of the MacPorts ports tree: a base commit on `master`, plus an ordered list of commits above it that together touch one or more ports. A changeset becomes one pull request.

Inside a changeset, **each commit is one logical change**, nearly always to one port: `jq: update to 1.8.1`, or `gdal: rebuild for poppler 25.09.0`. The MacPorts guide asks contributors to "minimize the number of commits … ideally with one commit per logical change". It also says that when a reviewer asks for changes, "please squash them to avoid unnecessary commits." Dockhand keeps that shape by construction. An edit to a port the changeset already changes is folded into that port's commit, not stacked on top of it.

Work moves through four stages. Every other command supports one of them.

```text
   author              shape               test                   submit
   ──────              ─────               ────                   ──────
   update              check               queue builds           push to your fork
   revbump             tidy                per port × macOS       open or update the PR
   checksums           undo                watch, logs
   create
   edit · save
      ▲                                                               │
      └──────────── review feedback · a failed build ◄────────────────┘
```

- **Author** commands add or change commits. They run every check that needs no VM (evaluation, lint, checksums, duplicate PRs) and take seconds.
- **Shape** commands give the history the form MacPorts asks for. They can also repair a branch that doesn't have it, which is most branches a newcomer makes by hand.
- **Test** puts builds on a **queue**. A **server** on your Mac works through the queue. Your terminal watches, and can stop watching at any time without stopping anything.
- **Submit** is the one verb that acts on GitHub for you: it pushes to your fork and opens or updates the pull request. It refuses a changeset that is untested or misshapen unless you tell it otherwise.

The common case stays one line:

```console
$ dockhand update jq --submit
```

## 2. Principles for the experience

1. **The changeset is the unit of work.** Everything you author lands in one, and everything dockhand shows is arranged by changeset. A port is what you edit, a commit is what a reviewer reads, and a changeset is what you ship.
2. **One commit per logical change, by construction.** Authoring folds each edit into its port's commit, so no contributor needs to learn `git rebase -i` to meet the guidelines. For a branch that didn't start that way, `tidy` rewrites it.
3. **Compose fast, test on the queue, submit deliberately.** Authoring answers in seconds, builds take minutes to hours, and publishing is public. They are three separate verbs, and each one says what it costs before it spends it.
4. **Git stays the truth, and stays yours.** A changeset is a real branch in a real worktree, so `git log`, `git diff`, and your editor all work. Use Git however you like; dockhand reads the branch and reconciles. Your main checkout is never touched.
5. **Every build goes through the queue, and something is always driving it.** A server, started on demand, owns running work. So detaching is always safe, a closed terminal never stalls a build, and a second terminal sees exactly what the first one sees.
6. **Speak the contributor's language.** Output uses port names, versions, pull request numbers, macOS release names, and changeset names. It never shows job or attempt IDs at the default level. Commit subjects are written the way maintainers write them.
7. **Every refusal carries the way forward.** A refusal names the command that gets past it, or the decision that only you can make. Each refusal has a stable code that `dockhand explain` expands.
8. **Evidence ticks boxes; people attest.** A PR checklist item is ticked only when dockhand recorded evidence for it. Items only a person can vouch for are asked, and recorded as the person's word.
9. **Reversible until it's public, careful once it is.** Every history rewrite can be undone with `dockhand undo`. Every push to an existing branch is conditional on what was there before. Dockhand pushes only to your fork, or to a contributor's PR branch when GitHub lets maintainers edit it.
10. **Humans and scripts alike.** Prompts appear only on a terminal. `--yes` answers them, `--json` gives every command one result document, and a non-terminal run never waits for input.
11. **Nothing outward-facing without being asked.** Only `submit` pushes and opens pull requests, only `test --on github` pushes to your fork, and commenting on other people's pull requests is a separate, explicit request.

## 3. The nouns

| Noun | What it is | How you name it |
| --- | --- | --- |
| **changeset** | A branch of the ports tree with its own worktree, commits, test results, and at most one PR. | Its name (`jq-1.8.1`), a port it changes (`jq`), or its PR (`#34901`). |
| **commit** | One logical change: to one port, or the same rebuild across several. Dockhand knows what kind of change it is: update, rebuild, checksums, new port, or other. | Rarely named directly; `status <changeset>` lists them. |
| **port** | A port or subport, as MacPorts names it. | `jq`, `py313-ipdb`, `terraform-1.16`. |
| **test** | Builds of a changeset's ports on one or more macOS releases, at one exact state of the changeset. | By changeset; `--on` picks where. |
| **queue** | Every requested build, in order, for all your changesets. | `dockhand queue`. |
| **server** | The process on your Mac that works through the queue. It starts on demand, or runs from login. | There is only one. `dockhand server` manages it. |
| **PR** | The pull request a changeset became. | `#34901`, or its URL. |

Some words are gone from the user's view: job, attempt, contribution, revision, change ID, evidence, and provider. They remain real in the engine, and `-v` and `--json` show them. At the default level, a person never needs one to type a command.

### Where changesets live

Each changeset that dockhand creates gets a **sparse worktree** of your ports clone. By default it lives in a visible directory beside the clone, `~/src/macports-changesets/<name>`, which `init` offers and `changesets` in the config moves. A worktree holds `_resources` and the directories of the ports the changeset touches, and it grows when you edit another port in it. So ten open changesets cost a few megabytes, not ten copies of a 40,000-port tree, and your main checkout stays on whatever you left it on.

The worktrees are ordinary directories and are meant to be used directly. Open `~/src/macports-changesets` in an editor and every open changeset is there side by side. Edit any file in one and run `dockhand save`, or run `git` in it as you would anywhere. Dockhand removes a changeset's worktree only after its PR merges or you `drop` it, and never while it has unsaved edits.

A branch you made yourself stays where you made it. If you run a dockhand command on it, dockhand offers to track it as a changeset, and its worktree is your checkout.

The **current changeset** is the one whose worktree you are in, or the branch checked out in the directory you're in. Outside any changeset, commands take a selector, and on a terminal they offer a picker when the choice is ambiguous. Dockhand never guesses from "the one you used last".

```console
$ dockhand path jq-1.8.1              # print the worktree path
$ dockhand shell jq-1.8.1             # a subshell inside it; exit to leave
$ dockhand open jq-1.8.1              # open it in $VISUAL
```

## 4. The command map

The six marked ★ are the everyday loop; most contributors rarely need the rest. Help lists commands in this order, grouped by stage, not alphabetically.

| Group | Command | What it does |
| --- | --- | --- |
| **Author** | ★ `update <port>… [version]` | Update to the newest release (or the one named): version, checksums, generated dependency blocks, and the revision reset to 0. |
| | `revbump <port>… "<why>"` | Increment the revision, with the reason as the subject: `gdal: rebuild for poppler 25.09.0`. `--dependents-of <port>` chooses the ports. |
| | `checksums <port>…` | Re-fetch the distfiles and record their checksums. A changed archive behind an unchanged name is reported as a stealth update, with what changed inside it. |
| | `create <name \| url>` | Start a new port from a forge URL, a package-registry name, or `--like <port>`, then open it in your editor. |
| | ★ `edit <port>` | Open the Portfile in your editor. When you close it, run `save`. |
| | ★ `save` | Fold your worktree edits into each port's commit, with checksums filled in and the subject written for you. |
| **Shape** | `check [--fix]` | Everything checkable without a VM: commit shape, subjects, lint, checksums, evaluation, and duplicate open PRs. `--fix` applies the safe fixes, and `--review` posts the findings on someone else's PR. `save` and `submit` run it too. |
| | `tidy` | Rewrite history into one commit per logical change, in dependency order, with the final tree unchanged. |
| | `undo` | Put back the history from before the last rewrite by `tidy`, `save`, `update`, or `rebase`. |
| **Test** | ★ `test [<port> [+variant…]]…` | Queue builds of the changeset's ports on the platforms in `--on`. Stays and shows progress; Ctrl-C detaches. |
| | `logs [<port>]` | A build's log in your pager, or only the error region with `--errors`, or followed live with `-f`. |
| **Submit** | ★ `submit` | Check, test if the current state isn't tested yet, push to your fork, and open or update the PR. |
| | `sync` | Read the PR's state, reviews, and CI now, instead of waiting for the server's next look. |
| **Changesets** | ★ `status [<changeset>]` | Everything open and what it needs from you, or one changeset in detail. Plain `dockhand` does the same. |
| | `watch [<changeset>]` | The same view, live: the queue, builds as they run, and keys for the verbs. |
| | `new <name>` | Start an empty changeset from fresh `master`, for work you'll author by hand or across several ports. |
| | `track <branch>` · `fetch <pr>` | Bring a branch you made, or someone's open PR, in as a changeset. |
| | `rebase` · `rename` · `drop` | Move onto current `master`, rename, or stop pursuing a changeset (its history is kept). |
| | `path` · `shell` · `open` | Get to a changeset's worktree. |
| **Discover** | `outdated` | Your ports (or any selection) with newer upstream releases, and whether dockhand can update each by itself. `update --outdated` acts on the list. |
| | `info <port>` | One port: versions, maintainers, open changesets and PRs, and what dockhand can automate for it and why. |
| | `dependents <port>` | What depends on a port, and how (library, build, run). |
| **Queue and server** | `queue` | The queue across all changesets. Subcommands cancel, retry, and move items. |
| | `server` | Run, start, stop, install at login, or inspect the local server. |
| **Setup** | `init` | First-run setup: MacPorts, GitHub login, fork, clone, build images, and config. Safe to rerun. |
| | `doctor` | Check all of that, and say what to fix. |
| | `images` | Prepare, list, and rebuild the macOS images builds run in. |
| | `auth` · `config` | GitHub login; settings. |
| **Housekeeping** | `gc` · `db` · `explain <code>` | Cleanup, database backup and check, and the long form of a refusal. |

Help is arranged the same way. The everyday verbs come first, and the rest are listed one line per group:

```console
$ dockhand help
dockhand: author, test, and submit changes to MacPorts ports

Everyday
  update      update a port to a newer release
  edit        open a Portfile, and save your edits when you close it
  save        fold your edits into the changeset
  test        build the changeset's ports
  submit      test if needed, then open or update the pull request
  status      what needs you (also plain `dockhand`)

Author        revbump · checksums · create
Shape         check · tidy · undo
Test          logs · watch
Changesets    new · track · fetch · rebase · rename · drop · sync · path · shell · open
Discover      outdated · info · dependents
Server        queue · server
Setup         init · doctor · images · auth · config
Housekeeping  gc · db · explain

dockhand help <command> for details · dockhand help journeys for walk-throughs
```

`dockhand help journeys` prints the walk-throughs in §5, so a newcomer's first reading doubles as a tutorial.

Selectors, flags, and output conventions are shared by every command and specified once, in [§8](#8-reference).

## 5. Journeys

Each journey below is a mock terminal session. Port names are real. Versions, pull request numbers, dependents, and durations are illustrative. Lines starting with `$` are typed; everything else is dockhand's output on a terminal, with color left out.

### 5.1 First run

```console
$ dockhand init
Let's get you ready to contribute to MacPorts.

  MacPorts     ✓ 2.11.4 at /opt/local
  GitHub       → open https://github.com/login/device and enter WDJB-MJHT
               ✓ signed in as ada
  Fork         ✓ ada/macports-ports
  Ports tree   ? no clone found. Clone your fork to ~/src/macports-ports? [Y/n] y
               ✓ cloned; macports/macports-ports is the upstream remote
  Changesets   ? keep changesets beside it, in ~/src/macports-changesets? [Y/n] y
  Your ports   ✓ 212 ports list @ada as a maintainer
  Builds       ! no build images on this Mac
                 builds will run on GitHub Actions in your fork until you add one:
                 dockhand images add          (macOS 26 Tahoe, ≈ 30 min, 45 GB)
  Server       ✓ starts by itself when there's work; stops when idle

Ready. Try:  dockhand outdated --mine
```

`init` writes `~/.config/dockhand/config.toml` and can be rerun at any time. `doctor` runs the same checks without changing anything, and each failed check ends with the command that fixes it.

### 5.2 Update one port

```console
$ dockhand update jq
jq 1.7.1 → 1.8.1   (GitHub tag jq-1.8.1)
  ✓ changeset jq-1.8.1, from master 4c1e2d0
  ✓ version, checksums (1 distfile, 1.6 MB)
  ✓ lint, evaluation; no other open PR changes jq
  ✓ commit  jq: update to 1.8.1
next: dockhand submit jq
```

```console
$ dockhand submit jq
jq-1.8.1 · 1 commit · not tested yet
  ◐ tahoe   jq   building  2m41s
```

The live line becomes the result when the build finishes:

```console
  ✓ tahoe   jq   lint ✓ build ✓ install ✓ tests ✓   4m12s
  ✓ pushed ada/macports-ports jq-1.8.1
  ✓ opened #34901  jq: update to 1.8.1
    https://github.com/macports/macports-ports/pull/34901
```

`dockhand update jq --submit` does both steps in one go. If the port is already current, it says so, creates nothing, and exits 0. Ctrl-C during the build prints `detached; the build continues on the server (dockhand watch jq)` and exits 130. The server finishes the build, and then opens the PR, because `submit` asked for that.

### 5.3 When dockhand can't edit the Portfile itself

Some Portfiles do things dockhand won't guess at. The refusal says what it stopped at, then hands you the one part only a person can do and keeps the rest.

```console
$ dockhand update openjdk21
✗ can't update openjdk21 by itself [hook-exec]
  java/openjdk21/Portfile:48: its pre-fetch hook runs `exec`, and dockhand won't guess what that changes
  change the version yourself and dockhand does the rest (checksums, revision, lint, commit):
    dockhand edit openjdk21
```

```console
$ dockhand edit openjdk21
  ✓ changeset openjdk21, from master 4c1e2d0
  … opening java/openjdk21/Portfile in nvim
  version 21.0.8 → 21.0.9; checksums still describe 21.0.8
  ? fetch the new distfiles and fill in the checksums? [Y/n] y
  ✓ checksums (2 distfiles, 212 MB)
  ✓ lint, evaluation
  ✓ commit  openjdk21: update to 21.0.9      (changeset renamed openjdk21-21.0.9)
next: dockhand submit openjdk21
```

`save` is what runs when the editor closes. It works the same after edits in any editor or IDE: run `dockhand save` in the worktree. Filling in checksums after a hand edit is the job `port bump` has done since MacPorts 2.6. Dockhand does it in the changeset, and follows it with the evaluation and lint checks.

### 5.4 Refresh checksums

A stealth update, where upstream replaced an archive without renaming it, is the usual reason. The first question a reviewer asks is what changed, so dockhand answers it.

```console
$ dockhand checksums croc
croc 10.2.4 · the distfile changed upstream without a new name (stealth update)
  was   sha256 1f3a…c2d9   size 7,114,391
  now   sha256 9b0c…77e1   size 7,114,508
  inside the archive: 1 file differs, go.sum (+2 −2)
  ✓ changeset croc-10.2.4-checksums, from master 4c1e2d0
  ✓ checksums; dist_subdir croc/10.2.4_1, so mirrors keep both archives
  ✓ lint, evaluation
  ✓ commit  croc: update checksums (stealth update)
```

The `dist_subdir` change is the one the MacPorts guide gives for a stealth update, `${name}/${version}_1`. When the archive is byte-identical and only the Portfile's record was wrong, dockhand says so and makes no `dist_subdir` change. The contents diff compares the old archive on the MacPorts distfiles mirror with the new one from upstream. `--show-diff` prints it in full.

### 5.5 Bump a revision

The reason is the subject, because that is how maintainers write it.

```console
$ dockhand revbump gdal "rebuild for poppler 25.09.0"
  ✓ changeset gdal-rebuild, from master 4c1e2d0
  ✓ revision 2 → 3
  ✓ commit  gdal: rebuild for poppler 25.09.0
```

Several ports take one reason, and each gets its own commit: `dockhand revbump inkscape pdf2svg "rebuild for poppler 25.09.0"`. The subject is refused if it repeats the port name. `--closes 71234` and `--see 71200` add Trac trailers with full URLs.

### 5.6 Author a new port

```console
$ dockhand create https://github.com/rift-dev/rift
rift 0.4.2 · Rust (Cargo.toml) · MIT · "Fast structural diff for config files"
  ? category [devel]: textproc
  ✓ changeset rift-new, from master 4c1e2d0
  ✓ textproc/rift/Portfile from the github and cargo PortGroups
  ✓ cargo.crates: 143 crates, from cargo2port
  … opening textproc/rift/Portfile in nvim
  ✓ checksums (1 distfile + 143 crates)
  ✓ lint --nitpick, evaluation
  ! no test suite enabled (test.run); only MacPorts' built-in test phase will run
  ✓ commit  rift: new port
next: dockhand test rift
```

Dockhand fills in what it can observe: the name, version, license, description, homepage, and the build system. The maintainer line comes from your config, `{@ada example.org:ada} openmaintainer`. For the rest it follows existing ports: `--like ripgrep` starts from the conventions of a port you name, not from a template. Generated files use the modeline, four-space columns, and layout that `port lint --nitpick` expects. `check` treats `--nitpick` findings as blocking for a new port, because the guide asks new submissions to fix them.

### 5.7 A coordinated changeset: a library and its dependents

This is the pull request the current design can't make: a library update, plus rebuilds of the ports that link against it, as separate commits in one PR.

```console
$ dockhand update poppler
poppler 25.08.0 → 25.09.0
  ✓ changeset poppler-25.09.0, from master 4c1e2d0
  ✓ version, checksums; poppler-qt5 and poppler-qt6 move with it (same Portfile)
  ✓ lint, evaluation
  ✓ commit  poppler: update to 25.09.0
  ! the last 6 poppler updates on master also rebuilt its dependents
    once built, dockhand will say whether this one changes libpoppler's install name
next: dockhand revbump --dependents-of poppler     (from inside the changeset)
```

The "last 6 updates" line is a fact from the tree's own history, not a prediction. The install-name comparison comes after the first build ([§5.8](#58-a-build-fails)).

```console
$ dockhand shell poppler-25.09.0
(poppler-25.09.0) $ dockhand revbump --dependents-of poppler "rebuild for poppler 25.09.0"
8 ports link against poppler (from the index at 4c1e2d0)
  space toggles · a all · enter done
  [x] gdal          [x] inkscape       [x] pdf2svg       [x] texlive-bin
  [x] evince        [x] pdfgrep        [x] py-poppler-qt5
  [x] zathura-plugin-pdf-poppler
  2 more use poppler only at run time and need no rebuild (--all lists them)
  ✓ 8 commits  <port>: rebuild for poppler 25.09.0
  · 6 of these ports have other maintainers; the PR will mention them
    (submit --skip-notification keeps a batch of rebuilds quiet)
```

The ports get a commit each by default, which keeps every commit revertable on its own. `--together` makes one rebuild commit instead, since the guide's rule is one commit per *logical* change and rebuilding a library's dependents is arguably one. Its subject lists the ports when they fit within MacPorts' 60-character limit, and otherwise dockhand asks for one. (The contracts review says the guide *asks* for separate commits here. It only says to minimize them, "ideally with one commit per logical change", so either shape passes `check`.)

```console
(poppler-25.09.0) $ dockhand check
poppler-25.09.0 · 9 commits · 9 ports (+2 subports)
  shape       ✓ one commit per port · ✓ poppler first, then what depends on it
  subjects    ✓ all follow "port: …"
  lint        ✓ 9 ports; --nitpick clean
  checksums   ✓ poppler (fetched) · 8 unchanged
  evaluation  ✓ each commit changes only what its subject says
  other PRs   ! #34888 also changes gdal (opened by @someone, 2 days ago)
next: dockhand test
```

```console
(poppler-25.09.0) $ dockhand test --on tahoe,sonoma
poppler-25.09.0 · tahoe, sonoma · 9 ports, in dependency order
  tahoe    ✓ poppler 6m02s  ✓ gdal 11m40s  ◐ inkscape build 8m…   ○ 6 more
  sonoma   ✓ poppler 6m31s  ◐ gdal build 4m…                      ○ 7 more
  ! libpoppler.152.dylib → libpoppler.153.dylib: the install name changed, so the rebuilds are needed
  ctrl-c detach · l logs · q quit
```

On each platform, ports build in one VM, in dependency order, from the changeset's own tree, so every dependent links against the new poppler and not a published binary. MacPorts CI does the same for a PR: one job per macOS release, building the changed ports from the PR's own tree.

`submit` asks for a PR title when there is more than one commit, and suggests one: `poppler: update to 25.09.0, rebuild dependents`.

### 5.8 A build fails

```console
  ✗ sonoma   inkscape   build failed in phase build, 14m20s
      src/extension/internal/pdfinput/pdf-parser.cpp:612: error: no matching function for call to 'GfxState::getLineDash'
      full log: dockhand logs inkscape --on sonoma
  ↳ checking whether it's new: inkscape at master 4c1e2d0 on sonoma …
  ✗ caused by this changeset: inkscape builds at master on sonoma
```

Dockhand builds the failing port at the changeset's base to tell the two cases apart, because a dependent that was already broken is common. It reports `pre-existing: inkscape also fails at master` when that's the case. `--no-baseline` skips the extra build, and `test.baseline` in the config sets the default.

You fix the build in the port you'd expect:

```console
(poppler-25.09.0) $ dockhand edit inkscape
  … opening graphics/inkscape/Portfile in nvim
  + files/patch-poppler-25.09.diff (new file)
  ✓ lint, evaluation; patch applies to inkscape 1.4.2
  ? inkscape's commit now does more than a rebuild. Subject? [inkscape: fix build with poppler 25.09.0]
  ✓ folded into inkscape's commit     (still 9 commits)
(poppler-25.09.0) $ dockhand test
  ✓ reusing 8 ports' results: their inputs are unchanged
  ◐ tahoe inkscape · sonoma inkscape
```

Only inkscape is rebuilt, and anything in the changeset that depends on it. Every other port's result still applies, because nothing it builds from has changed ([§9](#9-what-this-asks-of-the-engine)).

### 5.9 Review feedback

```console
$ dockhand status jq
jq-1.8.1 · #34901 · changes requested by @ryandesign, 2h ago
  "Please also drop patch-configure.diff; it was upstreamed in 1.8.0." (textproc/jq/Portfile:31)
  MacPorts CI  ✓ macOS 14  ✓ macOS 15  ✓ macOS 26
  1 commit · tested on tahoe ✓ (at the submitted state)
next: dockhand edit jq
```

```console
$ dockhand edit jq
  … opening textproc/jq/Portfile in nvim
  − files/patch-configure.diff
  ✓ lint, evaluation
  ✓ folded into jq: update to 1.8.1    (still 1 commit)
$ dockhand submit jq
  ✓ tahoe   jq   4m03s      (retested: the Portfile changed)
  ✓ updated #34901: 1 commit, pushed with lease
  ? ask @ryandesign to review again? [Y/n] y
  ✓ review re-requested
```

The PR stays one commit, and the contributor never typed `git commit --amend` or `git push --force`.

### 5.10 A stack of commits, and tidy

This is the most common feedback maintainers give newcomers. Here, someone made a branch by hand, pushed five commits, and was asked to squash them.

```console
$ cd ~/src/macports-ports            # on branch update-jq
$ dockhand tidy
update-jq isn't a changeset yet: 5 commits above master 4c1e2d0, all changing jq, with PR #34905.
? track it as changeset update-jq? [Y/n] y

jq · 5 commits → 1
  a1b2c3d  Update jq                          ┐
  d4e5f6a  fix checksums                      │
  0badf00  oops                               ├─▶ jq: update to 1.8.1
  1234abc  address review comments            │     Closes: https://trac.macports.org/ticket/71234
  5678def  bump revision                      ┘

  subject   written from what the commits do together: version 1.7.1 → 1.8.1
  kept      the Trac link from 1234abc
  unchanged the files: tidy rewrites history, never contents

? rewrite? [Y/n/e = edit the plan] y
  ✓ update-jq is 1 commit (dockhand undo puts the 5 back)
  ? update PR #34905 now? This replaces the branch's history on your fork. [Y/n] y
  ✓ #34905 updated: 1 commit, pushed with lease
```

`tidy` never changes the final tree, so a reviewer's diff is exactly what it was. Content problems belong to `check`. In this example it would add `✗ jq: revision is 1 after a version update (should be 0) — dockhand check --fix`.

With several ports, `tidy` also splits commits by port and orders them so that each port comes after the ports it depends on. Commits that each carry their own logical change to one port stay separate: `jq: update to 1.8.1` and `jq: add +docs variant` are two commits, and that's fine. The plan shows which commits it would fold and why, for example "fixes the checksum 1a2b3c4 set" or "subject reads like a follow-up". `e` opens the plan in your editor as one line per commit, like `git rebase -i` but grouped by port.

Prevention matters more than cure. In a changeset, `save` folds edits into the port's existing commit, and `submit` runs `check`, which refuses a misshapen changeset with `run dockhand tidy`, or `--as-is` if you mean it. A contributor who only ever uses `edit`, `save`, and `submit` can't produce the stack at all.

### 5.11 A maintainer takes over a contributor's PR

```console
$ dockhand fetch 34905
pr-34905 · "Update jq" by @newcontrib · 5 commits · jq · maintainers can edit
  ✓ changeset pr-34905 at ~/src/macports-changesets/pr-34905

$ dockhand check pr-34905
  ✗ shape      5 commits for one change to jq (MacPorts asks for one) → dockhand tidy
  ✗ subject    "Update jq" → jq: update to 1.8.1
  ✗ revision   1 after a version update; should be 0 → dockhand check --fix
  ✓ lint · ✓ checksums · ✓ evaluation
```

From here, the maintainer has two options. One is to fix the PR themselves:

```console
$ dockhand tidy pr-34905 --yes && dockhand check pr-34905 --fix && dockhand submit pr-34905
  ✓ tahoe jq 4m08s
  ✓ pushed newcontrib/macports-ports update-jq (maintainer edit, with lease)
  ✓ updated #34905: 1 commit
```

The other is to explain what needs to change, which often teaches a newcomer more:

```console
$ dockhand check pr-34905 --review
review of #34905, as @ada (write access to macports/macports-ports)
  summary                  5 commits for one change to jq; MacPorts asks for one per logical change.
                           To squash them: `dockhand tidy`, or `git rebase -i master`, then push
                           with `git push --force-with-lease`.
  commit a1b2c3d           subject "Update jq" should be "jq: update to 1.8.1"
  textproc/jq/Portfile:7   revision should be 0 after a version update
? post as: c comment · r request changes · e edit first · n cancel  r
  ✓ review posted on #34905: changes requested, 1 inline comment
```

- `--markdown` prints the same text for pasting anywhere.
- `--comment` posts it as a plain PR comment, which any GitHub account can do.
- `--review` posts it as a GitHub review, with each finding on the line it's about. The review suggests both dockhand and plain Git, since the contributor may not use dockhand.
- Requesting changes is offered only when your account has write or triage access to macports/macports-ports, because only then does the review count. Dockhand checks the permission and says why the option is missing when it is.
- Every post is shown in full and asks before it's sent. When you review the PR again after it changes, dockhand says which earlier findings are resolved instead of repeating them.

Authorship is kept: `tidy` keeps the contributor as the commit's author, and adds you only as committer.

### 5.12 Working through your ports

This is what dockhand is mostly for: one maintainer with many ports, keeping them current. Start with the list:

```console
$ dockhand outdated --mine
  PORT        NOW       NEWEST    DOCKHAND CAN
  croc        10.2.4    10.2.5    update
  fd          10.2.0    10.3.0    update
  jq          1.7.1     1.8.1     update
  lazygit     0.54.0    0.55.1    update
  xan         0.52.0    0.53.0    update (cargo2port)
  openjdk21   21.0.8    21.0.9    edit by hand: dockhand edit openjdk21
6 of 212 ports have newer releases · 3 couldn't be checked (-v says why)
```

Then act on all of it, and let whatever passes go through to a PR:

```console
$ dockhand update --outdated --mine --submit
6 ports: 5 changesets, 1 left for you (unrelated ports go in separate PRs)
  ✓ croc-10.2.5      checks pass → test → PR
  ✓ fd-10.3.0        checks pass → test → PR
  ✓ jq-1.8.1         checks pass → test → PR
  ✓ lazygit-0.55.1   checks pass → test → PR
  ✗ xan-0.53.0       stopped: patch-lto.diff no longer applies (2 hunks)     dockhand edit xan
  · openjdk21        needs a hand edit                                          dockhand edit openjdk21
4 changesets are on their way to a PR; you can close this terminal.
next: dockhand watch
```

An hour later, everything that passed is waiting on reviewers, and only what didn't pass is on your list:

```console
$ dockhand
Needs you
  ✗ lazygit-0.55.1   build failed on tahoe, caused by this change            dockhand logs lazygit
  ✗ xan-0.53.0       patch-lto.diff no longer applies (2 hunks)              dockhand edit xan
  ! openjdk21        21.0.9 is out; needs a hand edit                          dockhand edit openjdk21
Waiting on others
  ○ croc-10.2.5      #34902 opened 50m ago, CI ✓
  ○ fd-10.3.0        #34903 opened 35m ago, CI running
  ○ jq-1.8.1         #34901 opened 55m ago, CI ✓
```

Every changeset goes through the same gates on its own. It has to pass `check`, then build and install on the platforms in `test.on`, and only then is the PR opened. One failure stops only its own changeset, never the batch. The server carries each changeset through, so closing the terminal loses nothing, and the one PR per unrelated port that reviewers want comes out naturally.

To make this the standing behavior instead of a flag you remember, set it once:

```toml
[update]
then = "submit"      # stop (the default) · test · submit
```

With that, a plain `dockhand update --outdated --mine`, or `dockhand update jq`, carries every changeset that passes through to a PR. `--stop` on any one run holds it at the local checks. The server can do the whole loop daily without being asked, with `server.updates = "submit"`. It checks your ports for new releases every morning, prepares changesets, and sends whatever passes through to a PR. It opens at most `submit.daily-limit` PRs a day, ten by default, so a release wave doesn't land on reviewers all at once, and the rest wait in the queue for the next day. `server status` says plainly that it is opening PRs on your behalf.

Related ports share a changeset only when you ask for it, with `--into`, `revbump --dependents-of`, or by working inside a changeset.

### 5.13 After the merge

Nothing to type. The server looks at open PRs every few minutes, and the next `status` reports what it found:

```console
$ dockhand
✓ jq-1.8.1 merged by @ryandesign 1h ago as 7e3f9a1; worktree and branches removed
…
```

`dockhand sync` looks right away. A merged changeset's records stay, and `status --all` shows them. Its worktree, local branch, and fork branch are removed, each only if it still holds the commit that was merged. Anything else is kept and reported, never discarded.

## 6. Seeing everything: status and watch

### `dockhand` and `dockhand status`

With no arguments, dockhand answers "what needs me?"

```console
$ dockhand
You're in poppler-25.09.0: 9 commits, testing (tahoe 7/9 ✓, sonoma 4/9 ✓)

Needs you
  ! jq-1.8.1          #34901 changes requested by @ryandesign (2h)       dockhand edit jq
  ✗ xan-0.53.0        build failed on sonoma, caused by this change       dockhand logs xan
In progress
  ◐ poppler-25.09.0   testing, ≈ 25 min left
  ○ croc-10.2.5       queued (3rd), then submit
Waiting on others
  ○ ripgrep-14.1.2    #34877 open 3 days, CI ✓, no review yet
  ○ gdal-rebuild      #34880 waiting on maintainer @someone; the 72-hour timeout ends in 41h
Recently merged
  ✓ deno-2.5.1        merged yesterday

server: 2 of 2 VMs busy · 4 of your ports have updates (dockhand outdated --mine)
```

The sections are fixed: **Needs you**, **In progress**, **Waiting on others**, and **Recently merged**. Each "needs you" row ends with the command that addresses it. Observations say how old they are when it matters: PR state comes from the server's last look, and the view says so when that was a while ago.

`dockhand status <changeset>` is the full picture of one changeset:

```console
$ dockhand status poppler-25.09.0
poppler-25.09.0 · ~/src/macports-changesets/poppler-25.09.0 · on master 4c1e2d0 (14 behind)
not submitted · tested at the current state on tahoe; sonoma in progress

  COMMIT    PORT                      CHANGE                             TAHOE   SONOMA
  3f2a9c1   poppler (+qt5, +qt6)      update to 25.09.0                  ✓       ✓
  81b0d2e   gdal                      rebuild for poppler 25.09.0        ✓       ✓
  c0ffee1   inkscape                  fix build with poppler 25.09.0     ✓       ◐ build
  a4d1f00   pdf2svg                   rebuild for poppler 25.09.0        ✓       ○
  …         5 more                                                       ✓       ○

  checks    ✓ shape ✓ subjects ✓ lint ✓ checksums ✓ evaluation ! #34888 also changes gdal
next: wait for sonoma (dockhand watch), then dockhand submit
```

`--json` gives the same projection, and `-v` adds identifiers, full hashes, and times. One phrasebook produces the words for the table, the one-line summaries, `watch`, and the JSON state fields, so they can't disagree. That lesson from the current design is kept.

### `dockhand watch`

`watch` is the live, full-screen form. It observes only: the server does the driving, so opening or closing the view never starts or stops work. That fixes a surprise in today's console, which starts processing, pushes included, as soon as it opens.

```text
dockhand · ~/src/macports-ports · server: 2/2 VMs busy                            14:02
──────────────────────────────────────────────────────────────────────────────────────
  CHANGESET          PORTS  STATE                                   PR       UPDATED
▸ poppler-25.09.0    9      ◐ testing · tahoe 7/9 · sonoma 4/9      –        now
  jq-1.8.1           1      ! changes requested                     #34901   2h
  xan-0.53.0         1      ✗ build failed · sonoma                 –        20m
  croc-10.2.5        1      ○ queued 3rd, then submit               –        5m
  ripgrep-14.1.2     1      ○ awaiting review · CI ✓                #34877   3d
──────────────────────────────────────────────────────────────────────────────────────
  QUEUE  ◐ poppler-25.09.0 tahoe · pdf2svg build 2m    ◐ poppler-25.09.0 sonoma · inkscape build 9m
         ○ croc-10.2.5 tahoe   ○ xan-0.53.0 sonoma (retry)
──────────────────────────────────────────────────────────────────────────────────────
  ↑↓ select  enter details  t test  s submit  l logs  e edit  o open PR  x cancel  d drop  ? help
```

Each key runs the verb it names, with that verb's authority, and verbs that cost minutes or act on GitHub confirm first. `watch <changeset>` opens with that changeset expanded, and `watch --logs` adds a pane that follows the selected build's log.

## 7. The queue and the server

### Why a server

Today, whichever process is running drives the work. When the last dockhand process exits, nothing moves until another starts. A finished VM's result waits, a `cancel` sits unapplied, and a second terminal sees less than the first ([coordination note](../instance-coordination.md)). The reimagined dockhand has one driver: the **server**. Every other command is a client that submits and observes. That removes the whole class of problems and makes detaching safe by definition.

The server runs on your Mac and works for one person: you. It's built for a maintainer with many ports and one Mac to build them on. It opens no network port and has no accounts. It builds on no other machine except GitHub Actions in your fork, and only when you ask for that with `--on github`.

### Starting it

Nobody has to think about the server. The first command that needs one starts it, says so once, and it exits after a configurable idle period once the queue is empty:

```console
$ dockhand test
  · starting the server (it stops after 15 minutes with nothing to do)
```

To keep it running across logins, which is what a Mac that builds all day wants:

```console
$ dockhand server install          # a launchd agent; `server uninstall` removes it
$ dockhand server status
server · dockhand 1.0.0 · up 3h12m · pid 4711
  capacity    2 VMs (2 busy)
  images      tahoe (base, xcode) · sequoia (base) · sonoma (base)
  queue       5 builds (2 running)
  your ports  212 · checked for new releases daily at 07:00 · new releases become changesets
              that stop at the local checks (server.updates = "draft")
  PRs         following 3 open PRs, every 5 min
  notifies    on finished tests and PR changes (macOS notifications)
```

`dockhand server run` runs it in the foreground for a terminal or a supervisor, with its log on stderr. `dockhand server logs -f` follows a background server's log.

When no server can run, as in some CI containers, `--inline` runs the work in the command's own process, and an interrupt then really stops it. This is today's mode, kept as the degraded case, and dockhand says when it's in it.

### The queue

```console
$ dockhand queue
server · capacity 2

  #  CHANGESET          ON        STATE                             FOR
  1  poppler-25.09.0    tahoe     ◐ pdf2svg build, 7 of 9 done       you, attached
  2  poppler-25.09.0    sonoma    ◐ inkscape build, 4 of 9 done      you
  3  croc-10.2.5        tahoe     ○ queued                           submit
  4  xan-0.53.0         sonoma    ○ queued (retry after fix)         you
  5  pr-34910           tahoe     ○ queued                           a PR on your ports

$ dockhand queue move 4 --top
$ dockhand queue cancel 5
```

The queue follows three rules:

- **A queue item is a changeset at one exact state, on one platform.** Ports build inside the item, in dependency order.
- **The newest state wins.** When a changeset changes, its queued items are replaced. Running items finish, and their results still count for every port whose inputs didn't change.
- **You first.** Work you're attached to or asked for comes before work the server started by itself: daily updates, retests, and others' PRs. Otherwise, changesets take turns, so one big changeset doesn't starve a one-port fix, and a morning's thirty updates don't hold up the fix you're waiting on.

### Where builds run: `--on`

One flag says where a build runs, and replaces today's `--provider`, `--image`, and `--os`:

| `--on` | Builds on |
| --- | --- |
| `host` (default) | This Mac, in the image for its macOS release |
| `sonoma`, `14`, `tahoe,sonoma` | This Mac, in those releases' images |
| `all` | This Mac, in every release that has a prepared image |
| `github` | GitHub Actions in your fork, running MacPorts' own workflow |

On a Mac that can't run Tart images, `github` is how a changeset gets built before it becomes a PR. It is the MacPorts CI workflow, run on your fork before the PR exists: the runners it has (macOS 14, 15, and 26 today), default variants, `port lint` without `--nitpick`, no trace mode, and only the built-in test phase. Results from it say so. It's the lighter check, and the PR body doesn't claim more than it ran.

The port is evaluated where you are. Each build runs in the image for its release, and results are recorded per release. Whether a port needs full Xcode still picks the image automatically. A fallback is something you state in advance, never something dockhand infers: `--on tahoe --else github`, or `test.else = "github"` in the config. The result says which one served.

### What the server does besides building

These are all visible in `server status`, and each can be turned off:

- **Follows PRs.** Every few minutes it reads the state, reviews, and CI of your open PRs. It notices merges and cleans up after them, and turns changes into "needs you" rows.
- **Keeps master fresh.** It fetches `master` so that authoring starts from a recent base, and marks changesets that no longer rebase cleanly.
- **Keeps your ports current, as far as you let it.** Every morning it checks your ports for new releases. `server.updates` sets how far it goes:
  - `list`, the default, shows the count in `status`;
  - `draft` prepares a changeset for each update and stops at the local checks, so they're waiting for you;
  - `submit` carries whatever passes through to a PR, at most `submit.daily-limit` a day ([§5.12](#512-working-through-your-ports)).
- **Tests continuously, if asked.** With `server.retest = true`, every new state of an open changeset is queued for testing, making the server your personal CI.
- **Tests others' PRs on your ports, if asked.** `dockhand server prs add --maintainer @ada` queues checks and test builds of new PRs that touch ports you maintain, and `status` shows the results under "PRs on your ports", with `check --review` one keystroke away. It posts nothing by itself unless you turn on `--review` for that rule. Then it posts at most one review per PR head, and never requests changes without you. Strangers' Portfiles run only inside VMs, and no credentials ever enter the guest.
- **Tells you.** A macOS notification when a test finishes or a PR changes state (`server.notify`).
- **Cleans up.** It prunes old VMs, logs, and worktrees on the same rules `gc` applies, and never touches a changeset that is still open.

## 8. Reference

### Selectors

Wherever a command takes a changeset, dockhand resolves the argument as follows:

1. an exact changeset name: `jq-1.8.1`
2. a PR: `#34901`, `34901` for `fetch`, or the PR's URL
3. a port the changeset changes, if exactly one open changeset changes it: `jq`
4. `.`, or nothing: the current changeset

An argument that matches several changesets is refused with the list on a script, and offered as a picker on a terminal. Authoring commands take ports, not changesets. They add to the current changeset if you're in one, and otherwise create one: `--into <changeset>` adds elsewhere, and `--new` insists on a fresh changeset. If a port already has an open changeset elsewhere, dockhand says which one and asks, since two open PRs for one port is usually a mistake.

Test targets follow `port`'s own syntax, so variants read the way MacPorts users write them: `dockhand test jq +docs poppler -qt5`. `--variants each` builds every non-default variant of each port once, which covers the template's "most important variants" in one flag.

### The flag vocabulary

The same flag means the same thing on every command that has it.

| Flag | Meaning |
| --- | --- |
| `-n`, `--dry-run` | Show what would happen; change nothing, queue nothing, push nothing. |
| `-y`, `--yes` | Accept every prompt's default. Implied when stdin isn't a terminal, where a prompt with no safe default is an error instead. |
| `-d`, `--detach` | Queue the work and return at once. |
| `--on <where>` | Where builds run ([§7](#where-builds-run---on)). |
| `--else <where>` | Where builds run if the first choice can't take them. |
| `--into <changeset>`, `--new` | Where authoring lands. |
| `--stop`, `--test`, `--submit` | On authoring commands: how far this run goes, overriding `update.then`. |
| `--fresh` | Build again even when a result already applies. |
| `--tests declared\|required\|skip` | Declared tests run and are reported (the MacPorts rule, the default); `required` makes them decide; `skip` omits them. |
| `--all` | Include what is hidden by default (retired changesets, current ports, finished queue items). It never means "the whole tree"; that's `--everything`, on `outdated` and `info` only. |
| `--json` | One result document on stdout; progress as JSON lines on stderr. |
| `-v`, `-vv` | Identifiers and background work; then every step. |
| `-q` | Only results and errors. |

Flags that belong to one command are described with it: `--together` on `revbump`; `--variants` on `test`; `--markdown`, `--comment`, and `--review` on `check`; and `--as-is`, `--no-test`, `--draft`, `--type`, and `--skip-notification` on `submit`.

Global: `--tree`, `--prefix`, `--db`, `--config`, `--color never|auto|always`, and `--inline`. Each has an environment variable and a config key, and a flag beats the environment, which beats the config.

### What `check` looks at

`check` has two groups: **shape**, which is about the history, and **content**, which is about the Portfiles. Each finding is an error (✗), which blocks `submit` unless overridden, or a warning (!). `--fix` applies the fixes marked safe, and lists the rest with their commands.

| Check | Finding | Fix |
| --- | --- | --- |
| One change per commit | ✗ a commit makes different changes to two ports (the same rebuild across several is fine) | `tidy` splits it |
| No follow-ups | ✗ commits that fix an earlier commit's work, or whose subjects read like one ("fix", "oops", "address review") | `tidy` folds them |
| Order | ! a port's commit comes before the commit of a port it depends on | `tidy` reorders |
| Merge commits | ✗ merges in the changeset | `rebase` |
| Subjects | ✗ doesn't start with the port name and a colon; ! longer than 60 characters; ! doesn't say what changed ("update to latest", "fix") | `--fix` rewrites subjects dockhand wrote; others are asked |
| Tickets | ! a Trac ticket cited as `#71234` instead of its full URL | `--fix` writes it as a `Closes:` or `See:` trailer, after asking which |
| Revision | ✗ not 0 after a version update; ! bumped in a commit that only changes how the port builds (the guide says not to rev bump for build fixes) | `--fix` for the first; the second is shown |
| Version order | ✗ the new version sorts below the old one under `vercmp`, with no `epoch` change | shown, with the `epoch` line to add |
| Obsoleting | ✗ `replaced_by` added without a version, revision, or epoch change, so nobody sees the port as outdated | shown |
| Checksums | ✗ checksums don't match the distfiles | `checksums` |
| Lint | ✗ `port lint` errors (what MacPorts CI checks); ! `--nitpick` findings, which are ✗ for a new port | shown |
| Evaluation | ✗ the Portfile doesn't evaluate; ! a commit changes more than its subject says, such as a "rebuild" that also changes dependencies | shown |
| Shared files | ! a PortGroup in `_resources` changed; lists the ports that load it (MacPorts CI builds none of them) | shown |
| Maintainers | · you aren't the maintainer: says whether the port is `openmaintainer`, and that others may merge only after 72 hours | shown |
| Other PRs | ! an open PR changes the same port | shown, with links |
| Base | ! `master` has moved; ✗ the changeset no longer applies to it | `rebase` |

Findings have stable codes (`shape-mixed-commit`, `revision-not-reset`, and so on). `dockhand explain <code>` gives the long form, and the passage of the MacPorts guide or commit-message wiki it comes from.

### How dockhand reads a commit

`save`, `tidy`, `check`, and the subjects dockhand writes all rest on one thing: dockhand evaluates each changed port before and after a commit, and names the change by what actually changed.

| Observed change | Kind | Subject dockhand writes |
| --- | --- | --- |
| Portfile added | new port | `rift: new port` |
| `version` changed | update | `jq: update to 1.8.1` |
| only `revision` changed | rebuild | `gdal: <your reason>`, such as `rebuild for poppler 25.09.0`; asked for, never invented |
| the same revision change in several ports, with `--together` | rebuild | `gdal, inkscape, pdf2svg: <your reason>` if it fits in 60 characters; otherwise asked |
| only checksums or size changed | checksums | `croc: update checksums (stealth update)`; the reason in parentheses is asked for |
| `replaced_by` added, or Portfile removed | obsolete / remove | `foo: replaced by bar` / `foo: remove` |
| a PortGroup in `_resources` | PortGroup | `<group> portgroup: <your summary>` |
| anything else | other | asked for, with `port: ` filled in |

When a commit does several of these, the first row that applies names it, and the others are mentioned in the body only if you ask. A subject you wrote is never replaced without asking. Subjects follow the commit-message wiki: the port name and a colon, specific ("update to 1.8.1", never "update to latest"), and 60 characters at most. The body is wrapped at 72 columns. Trailers go last in this order: `Closes:`, `See:`, then dockhand's own `Generated-By:`. Ticket numbers are written as full Trac URLs, which is how both the wiki and the template ask for them.

### The pull request

`submit` writes the body in the shape of the [pull request template](https://github.com/macports/macports-ports/blob/master/.github/PULL_REQUEST_TEMPLATE.md), under the template's own headings, and fills in only what it has evidence for.

- **`#### Description`**: for one commit, its body. For several, a table of commit, port, and change.
- **`###### Type(s)`**: `bugfix`, `enhancement`, and `security fix` are ticked when the changeset says so. A CVE identifier in a commit ticks `security fix`, and `--type bugfix` names the others. MacPorts' bot finds "update" and "submission" by itself, from a title containing ": update" and from a new Portfile, so dockhand makes sure the title lets it.
- **`###### Tested on`**: the template's own lines for each release built, `macOS 26.1 25B78 arm64` followed by the Xcode or Command Line Tools version. Below them, a grid of port × release, with a pre-existing failure marked as one, and the MacPorts version each image ran.
- **`###### Verification`**: each item is ticked from recorded results or left unticked with the reason beside it:
  - `port lint`, `sudo port test`, and the `-vst` install come from the build logs. The install box is ticked only if trace mode really ran: MacPorts before 2.13 can't trace on arm64 macOS 15 and later, and then the box says so rather than being ticked.
  - "Commit Message Guidelines" and "squashed and minimized your commits" are ticked when `check` passed on the state being submitted.
  - "Checked that there aren't other open pull requests" is ticked when dockhand searched and found none, and otherwise links what it found.
  - "Tested basic functionality of all binary files" and "the most important variants" are asked, never inferred. On a terminal, `submit` asks, and a yes is recorded as your statement, not dockhand's. `--variants each` evidence ticks the variants box by itself.
  - Items that don't apply are removed, as the template's comment asks, rather than left unticked.

`--skip-notification` adds the template's `[skip notification]`, which stops the bot from mentioning maintainers. That's the courteous choice for a batch of trivial rebuilds, and dockhand suggests it for one.

`submit --dry-run` prints the title and body it would send. `--draft` opens the PR as a draft before testing finishes, so MacPorts CI starts early. Dockhand fills in Tested on when the results arrive, and `submit --ready` takes it out of draft.

A body someone has edited is kept. Only the span dockhand owns, from "Tested on" through "Verification", is ever regenerated, and only while the text there is still what dockhand wrote. The PR title is the commit subject for one commit, and asked for otherwise.

### Output

- **Two streams.** Progress goes to stderr and results go to stdout, always, so `--json` and pipes stay clean.
- **One voice.** On a terminal, the live lines collapse into the final result, so nothing is said twice. Off a terminal, each milestone is one plain line.
- **Symbols.** ✓ passed, ✗ failed, ! needs a look, ◐ running, ○ waiting, · note. `--color never` and `NO_COLOR` also switch the symbols to ASCII words: `ok`, `FAIL`, `WARN`, `run`, `wait`.
- **Identifiers at `-v`.** The default level uses the names people type.
- **One `next:` line** at most, naming the command that moves the work forward, and never a command that doesn't exist yet.
- **Durations** on every build line, and a total on every result.
- **JSON.** Every command writes one envelope, `{"command", "exit_code", "error", "result"}`, with snake_case keys throughout. With `--json`, stderr is JSON lines, `{"time","level","changeset","port","message"}`.
- **Exit codes**, unchanged from today: 0 when the work reached what was asked, 1 for errors and refusals, 2 for failed builds, 3 when something needs a person, and 130 for an interrupt. Ctrl-C exits 130 and says the work continues on the server. `--detach` exits 0 once the work is queued.

### Configuration

```toml
# ~/.config/dockhand/config.toml
tree = "~/src/macports-ports"
changesets = "~/src/macports-changesets"
maintainer = "{@ada example.org:ada} openmaintainer"

[update]
then = "stop"              # stop | test | submit: how far a passing changeset goes by itself

[test]
on = ["host"]              # or ["tahoe", "sonoma"], ["all"], ["github"]
else = "github"            # optional fallback, stated in advance
tests = "declared"
baseline = true            # when a port fails, build it at the base to tell new from pre-existing

[submit]
draft = false
rerequest-review = "ask"   # ask | always | never
daily-limit = 10           # PRs the server may open by itself in a day

[server]
idle-exit = "15m"
notify = true
retest = false             # test every new state of open changesets
updates = "list"           # list | draft | submit: what the daily release check does
updates-at = "07:00"
```

`dockhand config get/set/edit` manages it. `dockhand config` with no arguments prints the effective settings and where each came from.

### What dockhand promises

- It never pushes to `macports/macports-ports`. It writes only to your fork, or to a contributor's PR branch when GitHub says maintainers may edit it.
- Every push to an existing branch is conditional on the head dockhand last saw there.
- Every rewrite of a changeset's history keeps the previous head, and `undo` restores it. The last ten are kept per changeset.
- `tidy` never changes the final tree, and `save` only ever folds edits into the changeset they were made in.
- An existing PR body is kept; only dockhand's own section is ever regenerated.
- Accepted work is durable. Closing a terminal, putting the laptop to sleep, or restarting the server doesn't lose it.
- A failed or missing build is never presented as a pass. Publishing without one needs `--no-test`, and the PR says so.
- Dockhand writes to GitHub only through `submit`, `test --on github`, and the reviews, comments, and review requests you confirm. The server submits by itself only when `update.then` or `server.updates` says `submit`, and never beyond `submit.daily-limit`. Everything else only reads.

## 9. What this asks of the engine

The experience above rests on a few engine changes. Most of them are the recommendations the contracts review already made.

| The experience needs | The engine change | Review |
| --- | --- | --- |
| Changesets across ports; `revbump --dependents-of`; `tidy` | A contribution becomes an ordered list of commits, each scoped to one port directory (or one uniform rebuild across several), with `_resources` allowed; publication and evidence cover every changed port | §3.1 |
| `edit`, `save`, and `tidy` naming changes correctly | A per-commit semantic diff: evaluate each touched port before and after, and classify the change | new |
| One driver; detaching always safe; `watch` sees everything | The client/service split, on sessions and fenced leases, with an event journal for observers | [coordination](../instance-coordination.md) 1–3, §3.4 |
| `--on sonoma` from a Tahoe host; `--on all` | Separate the evaluation platform from the build platform in accepted work | §3.3 |
| Retest only inkscape after its fix; no rebuild after a rebase that changed nothing relevant | Evidence keyed by each port's build closure (its directory, `_resources`, its dependencies' Portfiles), beside the whole-tree key | §3.2 |
| `--else github` | Accepted work carries an ordered list of alternatives | §3.6 |
| Worktrees per changeset | Sparse worktrees, and the traced-read guarantee that makes a sparse tree safe to evaluate | §3.7 |
| Build in dependency order from the changeset's tree | One VM per platform builds a changeset's ports in dependency order, as MacPorts CI does | new |
| "caused by this change" vs "pre-existing" | Baseline builds at the changeset's base, recorded as their own results | new, in the spirit of the failure-attribution principle |
| "the install name changed" | Compare the built product's install names with the previous binary archive's | new |
| `then = "submit"`; `server.updates` | An authoring job's destination (stop, test, or submit), frozen at intake as `--to` is today; the daily release check creates ordinary jobs under the same rules, counted against a daily limit | extends today's `--to` |
| `check --review`, `--comment` | PR review and comment writes in the forge adapter, and a read of the account's permission on macports/macports-ports | new |

Today's durability and evidence rules all carry over: durable intent, idempotent submission, reconciliation before retrying, no guessed fallbacks, and conditional pushes. The reimagining changes what a person types and sees, and adds the multi-port unit. It doesn't loosen any of those rules.

## 10. From today's commands

| Today | Reimagined | What changed |
| --- | --- | --- |
| `bump jq` | `update jq --submit`, or `update jq` with `then = "submit"` | Authoring and submitting are separate verbs; `--submit` or the config joins them. |
| `bump jq --to branch` | `update jq` | Stopping after authoring is the default. |
| `bump jq --to verified` | `update jq && test jq` | |
| `bump --unverified` | `submit --no-test` | |
| `bump-revision jq --subject "…"` | `revbump jq "…"` | The reason is still the subject, now positional. |
| `checksums jq` | `checksums jq` | Adds stealth-update detection and the archive diff. |
| `verify` | `test` | Builds go through the queue; `--on` replaces `--provider`, `--image`, and `--os`. |
| `verify --working-tree` | `save`, then `test` | Worktree edits become commits first; there are no tests of uncommitted state. |
| `publish` | `submit` | |
| `adopt <branch>` | `track <branch>`, or implicit | Multi-commit, multi-port branches are accepted as they are; `check` and `tidy` handle shape. |
| `adopt --pr 34812` | `fetch 34812` | |
| `amend` | `save` or `edit` | No staging; edits fold into their port's commit. |
| `amend --squash`, `adopt --squash` | `tidy` | Port-aware, reorders, never changes the tree, undoable. |
| `rebase` | `rebase` | Works in the changeset's own worktree; no switching away. |
| `abandon` | `drop` | Asks whether to close an open PR; keeps history. |
| `sync` | `sync`, usually automatic | The server follows PRs. |
| `status`, `console` | `status`, `watch` | `watch` observes and never drives. |
| `wait`, `cancel` | `watch`, `queue cancel` | |
| `serve` | `server` | On demand, or at login. |
| `outdated`, `assess` | `outdated`, `info` | `outdated` says what dockhand can automate; `info` says why for one port. |
| `setup` | `init`, `images` | |
| `reassociate` | `rename`, or automatic | A changeset follows its branch. |
| `gc`, `db` | `gc`, `db` | |

## 11. Decisions and open questions

### Decided, 2026-09-24

1. **The server stays on your own Mac.** Dockhand is for an individual maintainer working through the ports they maintain, so the server serves one person on one machine. The first draft's remote build servers, reached over SSH, are removed, and a shared service for several contributors is out of scope. Testing others' PRs on your ports stays, as an opt-in feature of the same server.
2. **Changesets live in sparse worktrees**, in a visible directory beside the ports clone, so that they're easy to open, browse, and edit directly ([§3](#where-changesets-live)).
3. **`update` stops after the local checks** by default. A maintainer who wants whatever passes to go straight to a PR sets `update.then = "submit"`, or passes `--submit` for one run. `server.updates = "submit"` does the same daily for every port they maintain ([§5.12](#512-working-through-your-ports)).
4. **Reviewing others' PRs is allowed.** `check --comment` and `check --review` post what `check` found, after showing it and asking. Requesting changes is offered to accounts with write or triage access to macports/macports-ports ([§5.11](#511-a-maintainer-takes-over-a-contributors-pr)).

### Still open

1. **What does "passes" include?** This draft reads "whatever passes local checks goes straight to PR" as: passes `check`, then builds and installs on this Mac. If the intent is to open the PR after `check` alone and let MacPorts CI do the building, that is one more value, `then = "submit-untested"`, whose PRs say that no local build ran.
2. **Is "changeset" the word on screen?** Assumed yes. MacPorts contributors also say "branch" and "PR", and the three are nearly interchangeable here. The UI could lead with the PR number once one exists.
