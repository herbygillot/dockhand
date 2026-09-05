# The cohort live check

**Status: a procedure, not a design.** It is the one thing about the plural
verification substrate that cannot be proven where the substrate was built.
S11 changed what the guest actually runs — a marker file and a per-member argv
set where there was one bare `argv` — and everything asserting that change is
correct is a golden, a synthesized log, or a fake. No VM boots in that
workflow. This checklist is the maintainer booting one.

It has three parts and they are ordered on purpose:

- **A. The control run.** One port, which must be byte-for-byte the build
  dockhand has always run. If A fails, stop: the compatibility claim the whole
  step rests on is false, and B and C are describing a different tool.
- **B. The cohort that passes.** Two portdirs, one guest, two verdicts.
- **C. The cohort that breaks.** The member that fails, the member that is
  blocked on it, and the single environment kept between them.

Every step says what output means it worked and what output is a finding. A
finding here is not a step to retry: it is a defect to write down, with the
guest log attached, because the corpus at
`internal/engine/testdata/cohorts/` is entirely synthesized and a real capture
is worth more than any of it.

---

## 0. Before anything

```
git -C <dockhand checkout> status --short          # expect: clean, or only what you meant
make check && go test -race ./... && GOOS=linux go build ./... && go vet ./...
go build -o /tmp/dockhand ./cmd/dockhand
/tmp/dockhand doctor
tart list
```

- **Worked:** `doctor` reports tart present and at least one provisioned base;
  `tart list` shows the base and golden images and **no** `dockhand-worker-*`.
- **A finding:** `doctor` reporting a base it cannot clone. Not a finding, but
  stop anyway: a `dockhand-worker-*` already listed means both licence slots
  may not be free — delete it (`tart delete <name>`) or wait.

Everything below runs in a `macports-ports` checkout with your fork as a
remote. Use `/tmp/dockhand` explicitly so an older installed binary cannot be
the thing under test.

**Two things changed under this document after it was written, and neither is
a finding when you see it.**

*Status is attention-ordered now.* It leads with failures, then work waiting on
a slot, then what passed and wants a person, then held work, then the quiet end
states, then everything else. Parts B and C leave more than one branch standing
at once, so a failing branch sorting above a passing one is the ordering doing
its job. This document only ever asserts what a branch's own line **says**;
where a line appears among the others is not part of any check here.

*The notes are schema 3, and your checkout still holds schema-2 ones.* Until
`refs/notes/dockhand/verify` is cleared, this binary refuses every note already
on disk and every step below reads as broken for the wrong reason. Clear it
first — the branches themselves are untouched by this, and re-earning their
evidence is a verification, not a re-bump:

```
git -C ~/Source/macports-ports update-ref -d refs/notes/dockhand/verify
```

Run it with that path spelled out, not with `-C .`. Deleting a ref that does
not exist succeeds silently, so the same command aimed at the dockhand source
tree — which never had the ref — reports nothing and leaves the real notes
exactly where they were. The check that it worked is a count, not an exit
code:

```
git -C ~/Source/macports-ports notes --ref=dockhand/verify list | wc -l   # 0
```

If you would rather keep them readable by the old binary, back the ref up under
another name first; nothing in this document needs them.

---

## Two things that are true of every part below

**`discard` cannot delete the branch you are standing on.** Each part
creates its branch with `git switch -c`, which leaves you on it, so every
cleanup step below switches away first. Without that, `discard` fails with
`cannot delete branch … used by worktree`.

**Branch from local `master`, never from `origin/master`.** dockhand
computes what a branch changes as the diff from its merge base with the
*local* primary branch — `PrimaryBranch` is documented to never fetch, so
the local position is the answer, staleness included (D21). Meanwhile
`status` runs a retire sweep that can advance `origin/master` underneath
you when one of your own PRs merges. Cut a branch from `origin/master`
after that, and every upstream commit your local `master` has not caught
up to becomes part of what dockhand thinks your branch changes — it will
add those ports to the cohort and build them. Observed live: a branch
meant to hold `oniguruma6` and `jq` was submitted as `oniguruma6, jq,
mise`, because dockhand's own `mise` PR had merged two steps earlier.
Recorded in `docs/todo.md`.

**`promoted; no PR found` on a branch you never pushed is a finding
again.** It was a known false line through 2026-09-04 — retire read the
tracking config as proof of a push — and is fixed as of `0647471`: a
branch counts as pushed only where a remote-tracking ref says so.
Transcripts from earlier runs of this document show the false line;
a run on a current binary must not.

---

## A. The control run — one port must not have moved

**A1. Make a one-portdir branch by hand.** Any small, quick port you can
build; `devel/oniguruma6` is used below because part B needs it anyway.

**The branch has to be named inside the `dockhand/` namespace.** `verify`,
`shell` and `discard` take any branch name — `Engine.Resolve` accepts one
outright — but `status` enumerates `refs/heads/dockhand/*` and nothing else,
so a branch named anything else is verifiable and invisible. A4 below reads
the result off `status`, and would have nothing to read.

```
cd <macports-ports checkout>
git switch -c dockhand/live-solo-control master
printf '\n' >> devel/oniguruma6/Portfile      # any no-op edit; the diff only has to touch the portdir
git commit -am 'oniguruma6: no-op, live check'
```

**A2. Verify it.**

```
/tmp/dockhand verify dockhand/live-solo-control
```

- **Worked:** one line, naming one port and one release — `verify: submitted
  oniguruma6 on <Release> (job dockhand-worker-…)`.
- **A finding:** the line naming two ports, or two jobs starting.

**A3. While it is still building, look at what the guest was told to do.**

```
/tmp/dockhand shell dockhand/live-solo-control
# inside the guest:
ls -1 /tmp/dockhand-verify
cat /tmp/dockhand-verify/argv
cat /tmp/dockhand-verify/argv.lint
exit
```

- **Worked:** `ls` lists **exactly** `argv`, `argv.lint`, `log`, `state` —
  nothing else. `argv` holds four lines, `-d`, `-N`, `install`, `oniguruma6`.
  `argv.lint` holds two, `lint` and `oniguruma6`.
- **A finding, and the important one:** any `subject.*`, any `argv.0*`, any
  `state.0`, or an `argv` whose lines differ in content or order. A single-port
  verification must produce the file set it has always produced. This is the
  claim S11 is not allowed to break, and this is where it would show.

**A4. Let it finish.**

```
/tmp/dockhand status
tart list
```

- **Worked:** the branch reads `passed (<Release>)` with no port name in front
  of it — a change with one subject is named by its branch and nothing else —
  and `tart list` shows no `dockhand-worker-*`: a green environment is a
  wasted slot and goes back.
- **A finding:** a line reading `oniguruma6: passed (<Release>)`. That is the
  cohort rendering applied to one subject, and it moves every status golden.

**A5. Clean up before part B.**

```
git switch master
/tmp/dockhand discard dockhand/live-solo-control
```

---

## B. The cohort that passes

**B1. Make a two-portdir branch by hand.** A library and a dependent that
would need a revision bump when it moves — the shape the cohort exists for.

```
cd <macports-ports checkout>
git switch -c dockhand/live-cohort-pass master
# the library: any no-op edit is enough to make the portdir part of the change
printf '\n' >> devel/oniguruma6/Portfile
# the dependent: bump its revision by hand
$EDITOR sysutils/jq/Portfile                     # revision 0 -> revision 1, or add "revision 1"
git commit -am 'oniguruma6, jq: live cohort check'
git show --stat HEAD
```

- **Worked:** `git show --stat` names two files under two different
  `<category>/<port>` directories.
- **A finding:** nothing here — this is your own commit. But if both files are
  under one portdir, the branch is not a cohort and part B proves nothing.

**B2. Verify the branch.**

```
/tmp/dockhand verify dockhand/live-cohort-pass
```

- **Worked:** one line naming **both** ports and **one** job:
  `verify: submitted oniguruma6, jq on <Release> (job dockhand-worker-…)`. The
  order is alphabetical by portdir (`devel/oniguruma6` before `sysutils/jq`),
  which is deterministic and deliberately arbitrary — that it also happens to
  be dependency order here is a coincidence, not a guarantee.
- **A finding:** two jobs, two lines, or the old refusal — `one at a time for
  now`, or any message about the branch changing more than one portdir. That
  refusal was retired in S11; seeing it means the binary under test is not the
  one you built.

**B3. Look at the note while it builds.**

```
/tmp/dockhand status
```

- **Worked:** the branch shows **two** lines, one per member, each naming its
  port, `oniguruma6` first: `oniguruma6: verifying (38s) (<Release>)` and
  `jq: verifying (38s) (<Release>)`. One job, one environment, two runs. The
  word is `verifying` and not the wire state `running` — a submitted run
  renders its elapsed time instead of its bare state
  (`internal/render/render.go`) — and both members show the same elapsed time
  because they share one job, which is the claim.
- **A finding:** one line for two members; two environments in `tart list` for
  one branch; a run keyed by release alone.

**B4. Confirm the guest received a cohort instruction set.** This is the step
nothing offline can do.

```
/tmp/dockhand shell dockhand/live-cohort-pass
# inside the guest:
ls -1 /tmp/dockhand-verify
cat /tmp/dockhand-verify/subject.0
cat /tmp/dockhand-verify/argv.0
cat /tmp/dockhand-verify/argv.1
cat /tmp/dockhand-verify/state.0     # only once the first member has finished
grep -n 'dockhand subject:' /tmp/dockhand-verify/log
exit
```

- **Worked:** `ls` lists `subject.0`, `argv.0`, `argv.0.lint`, `subject.1`,
  `argv.1`, `argv.1.lint`, `log`, `state`, and `state.<i>` for each member that
  has finished. `subject.0` holds exactly `===> dockhand subject: oniguruma6`.
  `argv.0` holds `-d`, `-N`, `install`, `oniguruma6`; `argv.1` the same for
  `jq`. No bare `argv` and no bare `argv.lint`. The marker lines appear once
  each, in build order, `oniguruma6` before `jq`.
- **Also present, and not a finding:** `baseline`, `manifest.ports`, and
  `links.<i>` for members whose links were measured. These are the ABI
  machinery, which part D interrogates; part B only requires that they do not
  disturb the instruction set.
- **Read the markers here, not after.** `dockhand log` reaches into a live
  guest, and a cohort that passes releases its guest — so by B5 the log is
  gone and `log` correctly answers "no environment to reach". The failing path
  keeps its environment, which is why C4 can read the log after the fact and
  B5 cannot.
- **A finding:** a bare `argv` beside the numbered ones (two instruction sets,
  one guest); a marker file holding anything but one line; a `-s` in an argv
  nobody asked to build from source.

**B5. Let it finish, then read the verdicts.**

```
/tmp/dockhand status
tart list
```

- **Worked:** both members read `passed (<Release>)`, `oniguruma6` first;
  `tart list` shows no **new** `dockhand-worker-*` — one guest, released once,
  after **both** members were terminal. Any worker that predates this check is
  someone else's orphan and stays listed.
- **Expected here, and answered in part D:** an `ABI check unavailable` line
  naming `oniguruma6`, because a no-op edit does not move the version and no
  merge-base portdir was staged, so there is no "before" to measure. The note
  records this as `baseline_source: "none"`. It is not a finding in part B.
- **A finding:** the guest released while a member was still running; a member
  reading `errored — the guest reported no output for this subject` (the log
  announced one member and not the other, which no correct runner does); both
  members carrying the same lint line when their Portfiles lint differently.

**B6. Check the lint evidence is per member.**

```
/tmp/dockhand status --json | python3 -m json.tool | grep -A3 '"lint"'
```

- **Worked:** each member's run carries its own lint summary. If both ports
  lint clean this proves little; it is worth doing when one of them warns.
- **A finding:** one member's lint line copied onto the other. That is the
  whole-log read the section split exists to prevent.

**B7. Clean up.**

```
git switch master
/tmp/dockhand discard dockhand/live-cohort-pass
```

---

## C. The cohort that breaks

This is the part the synthesized corpus is least able to stand in for: what
one guest does when a member fails partway through, which members it goes on
to build, and which environment survives.

**C1. Make a three-member branch with the FIRST member deliberately broken,
and its revision moved.** First in build order, so that a dependent sorts
behind it; and a third member that depends on neither, so that there is
something the failure must *not* block.

```
cd <macports-ports checkout>
git switch -c dockhand/live-cohort-fail master
$EDITOR devel/oniguruma6/Portfile   # revision 0 -> 1, and add a line: build.cmd false
printf '\n' >> sysutils/jq/Portfile
printf '\n' >> sysutils/tree/Portfile
git commit -am 'oniguruma6, jq, tree: live cohort failure check'
git switch master
```

`build.cmd false` fails in the build phase and produces MacPorts' own
`Error: Failed to build oniguruma6: command execution failed`, which is the
exact shape the judge reads.

The revision has to move. With `_resources` carried in the overlay, MacPorts
reaches packages.macports.org, and a port whose version and revision match
an archive there is installed from that archive with no build phase at all:
run against `oniguruma6 @6.9.10_0` as it stands, the sabotage never
executes and all three members come back `passed` inside a minute (seen
2026-09-05; the finding is in `docs/todo.md`). At `_1` no archive exists
and the port builds from source. `jq` and `tree` are left at `_0` on
purpose — `jq` is never built, and `tree` installing from its archive is
still the runner going on past the failure.

`oniguruma6` is sabotaged rather than `jq` because `devel` sorts before
`sysutils`: the broken member has to be the one built first, or the
dependent is reached before its prerequisite and nothing is skipped.
`sysutils/tree` sorts after `sysutils/jq`, declares no dependencies, and
is the member the failure has no claim on.

**C2. Verify, and wait for it.**

```
/tmp/dockhand verify dockhand/live-cohort-fail
/tmp/dockhand status
```

Both from inside the ports checkout: dockhand's records are the
checkout's, and `status` run from anywhere else reports no dockhand
branches there.

- **Worked:** the note settles to
  - `oniguruma6: failed (<Release>) — environment kept: dockhand-worker-… —
    Failed to build oniguruma6: command execution failed`
  - `jq: blocked (<Release>) — oniguruma6 fails to build; this member is
    untested`
  - `tree: passed (<Release>)`

  The failing member owns the failure; the member that depends on it was
  skipped, is blocked, blamed on a sibling, and says so in those words —
  "untested", not "failed"; the member that depends on nothing that failed
  was built and passed. Three verdicts, one sentence about each.
- **A finding, and the one worth the most:** `jq` reading `passed` (a member
  that was never built recorded as evidence); `jq` reading `failed` (a member
  disproven by a sibling's breakage); `tree` reading `blocked` (the runner
  stopped at the failure, or a stranger's failure was laid on a member that
  does not depend on it — the shape D25 retired); either member blocked on a
  *dependency* sentence — `dependency oniguruma6 fails to build; the change
  itself is untested` — which means the roster match failed and this change's
  own breakage was read as a stranger's.

  Note that `jq` really does depend on `oniguruma6`, which does **not** soften
  the sentence check. `BlockedByMember` and `BlockedDetail` are chosen by
  roster membership, not by the dependency graph (`internal/verdict/log.go`):
  a blocker that is a member of this cohort must get the sibling sentence
  whatever the ports tree says about it. What the graph decides is only
  *which* members are skipped (`verify.Request.Requires`, computed at submit
  from the reverse index over the members being built). The give-away half
  is the ending — "this member is untested" against "the change itself is
  untested".

  A related signal worth reading: if `jq` is *built* — its marker appears in
  the log, and it fails with MacPorts' own dependency error rather than
  being blocked — the graph did not reach the guest, and the runner attempted
  a member whose prerequisite had already failed.

**C3. Confirm the runner's record, and that the environment is kept exactly
once.**

```
tart list
/tmp/dockhand shell dockhand/live-cohort-fail
# inside: cd /tmp/dockhand-verify; cat state state.0 state.1 state.2 requires.1; ls -1
exit
```

- **Worked:** exactly **one** `dockhand-worker-*`, and the shell lands in it.
  `state` says `failed`; `state.0` says `failed`; `state.1` says `skipped`
  and then `0` on its own line — the position of the member it was skipped
  behind; `state.2` says `passed`. `requires.1` holds `0`, and `requires.0`
  and `requires.2` are empty: that is the graph as submit spelled it.
- **A finding:** no worker (the failure's debug environment was handed back);
  two workers for one branch; no `state.1` (the runner stopped at the
  failure); a `state.1` that says `failed` or `passed` (the runner built a
  member whose prerequisite had failed); a `state.2` that is absent or says
  `skipped` (the runner did not go on, or skipped a member with no failed
  prerequisite).
- **Also present, and not a finding:** the ABI staging — `baseline`,
  `manifest.ports`, and per-member `manifest.<i>`, `links.<i>` and
  `probe.<i>`. A failed cohort keeps more of these than a passing one does,
  because nothing consumed them; part D is where they are read.

**C4. Confirm the log's attribution.**

```
/tmp/dockhand log dockhand/live-cohort-fail | grep -n 'dockhand subject:\|^Error:'
```

- **Worked:** two markers and one error, in this order: `===> dockhand
  subject: oniguruma6`, the `Error: Failed to build oniguruma6` line, then
  `===> dockhand subject: tree`. No marker for `jq`: a skipped member prints
  nothing, and its state file is its only record.
- **A finding:** a marker for `jq`; no marker for `tree` (the runner
  stopped); a marker printed before the failure it should follow.

**C5. Confirm the gate refuses to publish it.**

```
/tmp/dockhand promote dockhand/live-cohort-fail --no-pr
```

- **Worked:** it refuses, and says the verification failed. Do **not** pass
  `--no-verify`.
- **A finding:** anything other than a refusal — in particular the branch
  appearing on your fork, which means the gate let a failed verification
  through.

`--no-pr` is there to bound the blast radius, not to soften the test.
`verdict.DecidePublish` never sees the flag, so the gate decision is
identical; the evidence gate also returns before `ForkRemote`, the pull
request lookups and the push, so a working gate costs no network at all.
What the flag changes is only what a *broken* gate would do: push to your
own fork, which `git push <fork> --delete <branch>` undoes, instead of
opening a pull request against `macports/macports-ports` and notifying its
maintainers. Note that `origin` in this checkout is the canonical
repository, not a fork — `ForkRemote` resolves the fork by matching your
`gh` login against each remote's owner.

**C6. Clean up.**

```
git switch master
/tmp/dockhand cancel dockhand/live-cohort-fail     # releases the kept environment
/tmp/dockhand discard dockhand/live-cohort-fail    # removes the branch and its note
tart list                                 # expect: no dockhand-worker-*
```

---

## If a step fails

1. **Capture before cleaning.** The guest goes away when you cancel.

   ```
   /tmp/dockhand log <branch>        > /tmp/cohort-<shape>.log
   /tmp/dockhand status --json       > /tmp/cohort-<shape>.json
   /tmp/dockhand shell <branch>      # ls -1 /tmp/dockhand-verify; cat each state file
   ```

2. **Turn the capture into a corpus case.** The whole cohort corpus is
   synthesized and every file in it says so; a real capture replaces one
   outright.

   ```
   cp /tmp/cohort-<shape>.log internal/engine/testdata/cohorts/<shape>.log
   $EDITOR internal/engine/testdata/cohorts/<shape>.expect   # see the README there
   go test ./internal/engine/ ./internal/verdict/
   ```

   Keep the file name if you are replacing an existing shape, and rewrite its
   sidecar's provenance comment — that is what the README asks for.

3. **Do not re-record a golden to make the failure go away.** A single-port
   golden that moved is the defect, not the record of it.

4. **Recover the machine.** In order: `dockhand cancel <branch>` (releases the
   worker), `dockhand discard <branch>` (drops the branch and its note),
   `tart list` and `tart delete <name>` for anything left behind, and
   `dockhand status` once more — its orphan audit names workers no note
   accounts for.

---

## What this check cannot answer

Two things S11 deliberately left to a ruling rather than a run, both worth
holding in mind while doing the above:

- **The guest log is written by the change under test.** Per-member
  attribution reads marker lines out of a file that a port's own build output
  shares. A port that printed `===> dockhand subject: <name>` itself would move
  the attribution; the per-member `state.<i>` files exist and would corroborate
  it, and nothing reads them yet. Whether that trust boundary needs closing is
  a maintainer's call, not a test's — see `internal/verify/tart/tart.go`,
  `cohortRunner`.
- **Build order is only proven on the failing path.** A cohort that passes
  releases its guest, and the log goes with it; the note records each
  member's state, evidence, lint and probes, but not the order they ran in.
  B4 reads the markers from inside the live guest, and C4 reads them from the
  environment a failure keeps. Nothing durable records the order after a
  green run.

- **A revbump cohort orders itself differently, and must.** Part B's rule
  below is for a cohort derived from a diff. A cohort built by
  `bump-revision --for` puts the **headline first** and its members after it
  by port name: observed on libraw, `gnome/gthumb` came last though `gnome`
  sorts before `graphics`. That is not a lapse in the portdir rule, it is a
  different construction — the library has to be built before the dependents
  that link it, or they bind the old copy from the archive and the cohort
  proves nothing. What neither ordering does is sort the *dependents* among
  themselves topologically, so a dependent that depends on another dependent
  is still built in name order.

- **Build order is alphabetical by portdir, not topological.** In part B,
  `oniguruma6` is built before `jq` because `devel` sorts before `sysutils`, and
  the dependent going first is decided by a category name. It is deterministic
  and blame does not depend on it — the judge matches the log's name against
  the roster, not against a position — but ordering members by declared
  dependency is a real improvement waiting for a step of its own.

---

## D. The ABI baseline

Parts A–C prove what the guest builds. This part proves what it *measures*,
and it is the one thing in S12 that no test here can reach: the baseline is a
download of a published binary archive into a booted VM, and everything
offline stands on `verifytest.Fake` or on otool output captured on the host.

The recipe under test, in the order the guest runs it:

| # | Command in the guest | Why it is that command |
|---|---|---|
| 1 | stage the **merge base's** portdirs into `/tmp/dockhand-overlay` | the guest's own tree is frozen at provisioning and may not hold the version the change is leaving |
| 2 | `sudo -n port -N -b install <headline> <variants>` | `-b` installs the published archive and **refuses to build**; the variants are the branch build's, because an archive is named by version, revision and variants |
| 3 | `port -q installed <headline>` | port(1)'s exit status cannot answer this: on a port that is not installed it exits **0** and prints nothing |
| 4 | `port -q contents` + `otool -arch all -D` + `otool -arch all -L`, framed into `/tmp/dockhand-verify/manifest.pre` | `-arch all` so a universal file always reports every slice; batched through `xargs -0` because a per-file loop is minutes |
| 5 | `sudo -n port -N -f uninstall <headline>` | `-f` because MacPorts otherwise refuses while dependents are installed and asks a question a detached runner never sees; the dependencies stay, which is what the branch build would have pulled |
| 6 | stage the **branch's** portdirs over the same overlay, then launch | steps 1 and 6 write the same directory, which is why 1–5 come first |

**D1. Mint a branch with `bump`. A hand-made branch cannot reach this
part at all.** The baseline is staged only when the ledger holds a record
whose `Base.Sha` is set (`Engine.manifestAsk`, `internal/engine/submit.go`);
a branch made with `git switch -c` has no minted record, so `Base.Sha` is
empty, no merge-base portdir is staged, and every step below reports
`baseline_source: "none"` for a reason that has nothing to do with what D
is testing. Parts A–C showed this live: part B's cohort declined with
"no merge-base portdir was staged" for exactly this reason.

The headline also needs **dependents** — `manifestAsk` collects nothing for
a port with an empty reverse index — and its **merge-base** version needs a
published binary archive for this platform, because that is the version
step 2 installs.

```
cd <macports-ports checkout>
/tmp/dockhand bump <port>            # or --to <version> to pin one
```

- **Choosing the port is yours:** it must have dependents, a newer upstream
  release than the tree holds, and an archive for the version it is
  *leaving* on this OS and arch. `oniguruma6` satisfies the first; the
  other two depend on the day and the buildbot.
- **A finding before you start:** if the mint refuses, that is part E's
  subject, not D's — read the refusal and pick another port.

**D2. Watch the submit take longer than eleven seconds.**

`bump` submits background verification itself — `--no-verify` is what
suppresses it — so D1's mint is also the submit, and the clock belongs on
that one command:

```
time /tmp/dockhand bump <port>
```

- **Worked:** the submit returns having downloaded and activated the archive —
  tens of seconds, not eleven, and not minutes. Nothing compiled: `-b` selects
  only `depends_lib` and `depends_run`.
- **A finding:** a submit that takes *minutes*. That is a source build, which
  means `-b` was dropped and the "before" is a build nobody ever shipped.

**D3. Read what the environment recorded, while it is still there.**

```
/tmp/dockhand shell dockhand/<port>-<version>
# inside the guest:
cat  /tmp/dockhand-verify/manifest.ports
cat  /tmp/dockhand-verify/baseline
head -20 /tmp/dockhand-verify/manifest.pre
tail -1  /tmp/dockhand-verify/manifest.pre
port -q installed oniguruma6
ls /tmp/dockhand-overlay/devel/oniguruma6
exit
```

- **Worked:** `manifest.ports` holds the roster, headline first, one per line.
  `baseline` holds exactly `archive` and nothing else. `manifest.pre` opens
  with `===> dockhand manifest: port` and **ends with**
  `===> dockhand manifest: end` — the closing line is the whole durability
  guarantee, and a capture without it is refused rather than parsed.
  `port -q installed oniguruma6` shows the **branch's** version or nothing at
  all, never the merge base's: step 5 took the baseline back off.
  The overlay holds the branch's Portfile, not the merge base's.
- **A finding, and the worst one available:** `port -q installed` still showing
  the merge base's version. The old version left active makes the branch's
  install a no-op or an upgrade, and the run would pass without ever building
  the change. dockhand is supposed to refuse the submit outright in that case —
  see `internal/verify/tart/baseline.go`, "would not uninstall".
- **A finding:** `manifest.pre` present while `baseline` says `none`, or the
  reverse; a `manifest.pre` with no `end` line.

**D4. Confirm the measurement is of the merge base and not of the change.**

```
grep -A2 'dockhand manifest: version' /tmp/dockhand-verify/manifest.pre   # in the guest
```

- **Worked:** the version is the one the branch is leaving — the merge base's,
  not the branch's. This is the single fact the whole comparison rests on.
- **A finding:** the branch's version. The baseline was taken after the staging
  and the change was measured against itself, which always reports that
  nothing moved.

**D5. Now the honest refusal: a merge-base version with no archive.**

Requires the `_resources` staging fix (2026-09-03). Before it, *every*
port declined here for the wrong reason — "no usable archive sites
configured", meaning no site was ever consulted — which is not the same
answer as "this port has no archive" and cannot stand in for it. If you
see that sentence again, the binary under test predates the fix.

Note what has to be missing. Step 2 installs the **merge base's** version,
so the port that exercises this is one whose *current* version has no
published archive on this OS and arch — not one bumped to a version that
was never built. The branch's own version is never installed with `-b`.
On a young OS release many ports qualify; on a settled one, few do.

This branch must be minted too, for the same reason as D1: a hand-made
branch declines earlier, with the wrong reason, and never reaches the
archive lookup.

```
cd <macports-ports checkout>
/tmp/dockhand bump <a port whose current version has no archive here> --to <newer version>
/tmp/dockhand verify dockhand/<port>-<version>
```

If D1's own port declines with MacPorts' archive-not-found error rather
than measuring a baseline, that *is* this test — swap the two roles and
find a better-archived port for D1 instead of hunting a worse one here.

- **Worked:** the submit **succeeds** — a check that could not be made is not a
  submit that failed — and in the guest `/tmp/dockhand-verify/baseline` reads
  `none` on the first line with MacPorts' own refusal quoted on the second:
  `Error: Failed to unarchive …: Archive for … not found, required when
  binary-only is set!`. No `manifest.pre`.
- **A finding:** a submit that refused; a `baseline` file saying `none` with no
  reason under it; an empty `manifest.pre` written anyway. An empty baseline
  compares as every library removed, which is the strongest false break there
  is, and detecting the refusal by name is the only thing standing between that
  and a nine-port revbump proposal nobody asked for.

**D6. Confirm the settle reads both sides.**

```
/tmp/dockhand status --json | python3 -m json.tool | grep -B2 -A6 baseline_source
```

- **Worked:** the run carries `baseline_source` of `archive` (or `none` with a
  reason), a `baseline` manifest where there is one, a `manifest` for what was
  installed, and `links` naming the members bound to the headline's install
  names — and nothing else in `links`: a dependent links against libSystem too,
  and a map carrying that answers a question nobody asked.
- **A finding:** `baseline_source` empty. "We did not look" and "we looked and
  there was nothing" are different answers, and only one of them is an answer.

**D7. Clean up.**

```
git switch master
/tmp/dockhand cancel dockhand/live-baseline ; /tmp/dockhand discard dockhand/live-baseline
/tmp/dockhand cancel dockhand/live-baseline-none ; /tmp/dockhand discard dockhand/live-baseline-none
tart list
```

### What part D cannot answer

The compatibility-version ruling. `ABIDelta` treats an install-name change, a
removal and a **decreasing** compatibility version as breaks, and an increasing
one as widened — because dyld requires the loaded library's compatibility
version to be at least what the dependent recorded, so an increase is not a
load-time break. Whether an increase should still earn a revbump proposal is a
maintainer's call and no VM can settle it.

---

## E. The acceptance test — a proposal, and the two ways to answer it

Parts A through D prove the substrate: one guest, per-member verdicts, and a
manifest with an honest before. Part E is the thing the whole overhaul was
asked for — *"bundle revbumps to downstream ports if we are currently doing a
bump update to a port that ends up changing the target port's ABI"* — and the
part of it no fake can reach is the sequence: a real bump, a real measurement,
a proposal a person reads, and a second commit that only exists because they
accepted it.

Everything below is over the real reverse index, which means the port has to
be one that genuinely has dependents. Pick a **small library with few
dependents and a soname that actually moves**; the walkthrough's `libwidget`
does not exist. `graphics/libgeotiff` (four dependents) and `devel/brotli` are
the two the maintainer's own tree makes cheapest. A port with eighty
dependents is a correct answer and an evening.

**E1. Make sure the tree has a PortIndex, then bump the library.**

The reverse index is read out of `PortIndex` at the tree root. That file is
**generated by `portindex(1)` and is not carried in a macports-ports clone**,
so a fresh checkout has none — and with none there is no cohort decision to
make. Check it first; every measurement below rests on it.

```
cd <macports-ports checkout>
ls -l PortIndex PortIndex.quick        # both must exist
portindex                              # ~1-2 minutes if they do not
/tmp/dockhand bump <the library> --to <a version whose dylib soname moves>
/tmp/dockhand status
```

- **Worked:** a branch, and a run building. Exit 0 or 60.
- **Also worked, and newer than this document's first draft:** `held at mint:`
  followed by a reason naming the target as a prerelease, and exit 23. A change
  minted against a prerelease is born held now, so if the version you picked is
  an alpha, beta or release candidate, that is the hold doing its job and not a
  failure. Either pick a stable target, or `/tmp/dockhand unhold <branch>` and
  carry on — the measurement is the same either way.
- **A finding:** a decline. Pick another version — E needs a bump that reaches
  a build, and which version that is, is a fact about the port.
- **A finding:** `portindex` refusing, or `PortIndex` older than the checkout.
  A stale index proposes a cohort over yesterday's tree, which is a member list
  that may name ports the branch tip does not carry — E5 declines those by
  name, but the roster is still the wrong roster.

**E2. Let it settle, and read the proposal.**

```
/tmp/dockhand status
```

- **Worked:** under the branch, three lines in this order — the verdict, then
  `ABI changed: install name … → …, measured between …@<old> (binary archive)
  and @<new> (source not recorded) on <release>`, then `proposal: N dependents
  need a revision bump (…) — `dockhand bump-revision --for <branch>` builds the
  cohort, `dockhand dismiss <branch>` records that you looked and said no`.
  The exit status is **0**: a proposal is advisory and human-gated, and a
  status that failed over one would make the tool something to avoid running.
  The second half of that sentence says `(source not recorded)` rather than
  `(built from source)` unless the run was `--recheck` or a checksum refresh.
  Nothing in a manifest records how an installation was produced, so the
  criterion states the absence instead of assuming; `(built from source)` on a
  plain bump would be a claim nobody measured.
- **A finding, and the most important one in part E:** a proposal whose
  criterion says `ABI unchanged` or `ABI check unavailable`. Nothing is
  proposed on either, by construction — if a proposal appears beside one of
  those sentences, the gate between the measurement and the cohort has gone.
- **A finding, and the likeliest way this whole part quietly passes:** **no
  proposal and no `ABI` line at all**, on a port that has dependents. A leaf
  port settles exactly that way on purpose — the measurement's one consumer is
  the cohort decision — so silence here means either the index says nothing
  depends on the library (check with E3's `port echo depends:`) or the index
  could not be read. The second one now says so: an unreadable index records
  `ABI check unavailable: the ports tree's reverse index could not be read …
  (run `portindex` in the tree to generate one)`. If neither sentence appears
  and `port echo depends:` names ports, the gate between the tree and the
  finding has gone.
- **Also a finding:** a member in the proposal that lives in the headline's own
  portdir. That is a port this change is already editing, and a cohort that
  planned it would edit the Portfile it just changed.

**E3. Check the roster against the tree by hand.** This is the step that
cannot be automated, because it is the check on the automation.

```
port echo depends:<the library> | sort          # or grep the PortIndex
/tmp/dockhand status --json | python3 -m json.tool | less   # findings[].candidates
```

- **Worked:** every proposed member declares the library under `depends_lib` or
  `depends_run`; every `depends_build`-only dependent appears with
  `"proposed": false` and the reason saying so; every candidate's `portdir` is
  a directory that exists, **including for subports** — `php80-Judy` must read
  `php/php-Judy` and not `php/php80-Judy`.
- **A finding:** a member missing from the proposal that `port echo depends:`
  names. A dropped `lib:`, `bin:` or `path:` token is a dependent left broken
  with nothing said about it, which is the one failure this step cannot
  tolerate.

**E4. The machine gate refuses; a person is allowed past.** There is no
unattended publisher yet, so this is checked from the other side: a human
promote must say what it is publishing past.

```
/tmp/dockhand promote <branch> --no-pr
```

- **Worked:** on stderr, `promoting with dependent-revbump still proposed;
  `dockhand dismiss <branch>` records that you looked and said no`, and the
  push proceeds. A person is looking at the proposal, and promoting anyway is
  their answer.
- **A finding:** a refusal. Exit 24 is the machine's gate and must never meet a
  person at the keyboard.
- Undo it before continuing: `git push <fork> --delete <branch>`.

**E5. Accept the proposal.**

```
/tmp/dockhand bump-revision --for <branch>
git -C . log --format='%H %s' origin/master..<branch>
git -C . show --stat <branch>
```

- **Worked:** the new tip on stdout, **one** additional commit, and its diff is
  one `revision` line per member portdir and nothing else. The subject reads
  `<library>: revbump N dependents of <library> <version>`, and the body opens
  with the criterion from E2 **word for word**, followed by the caveat
  (`this criterion is necessary and not sufficient…`), the members with their
  reasons, and the ports examined and not bumped.
- **A finding:** two commits; a diff touching anything but `revision` lines; a
  modeline inserted into a dependent's Portfile (a cohort carries no riders);
  a criterion in the body that is a paraphrase of the one status printed. One
  claim, said once, is what lets a reviewer check it with `otool` by hand.
- **A finding:** a member edited whose Portfile in your working tree differs
  from the branch's. The plan is made from the branch tip's blob, never the
  worktree; the way to provoke it is to edit a member's Portfile in the
  worktree before running this and confirm the commit ignores your edit.

**E6. The members that declined.** *Untested as of 2026-09-04, and accepted
as a gap: across four cohorts on libraw and nettle, no member ever failed
to plan, so the "not bumped — plan: declined" road never ran. It is
covered offline by the planner's own tests; what has not been seen is the
verb naming a live decline on stderr and the note's candidate row saying
"proposed, then declined". Provoking one would mean a member whose
Portfile the planner cannot rewrite, which is contrived by construction.*

- **Worked:** every member the planner could not plan is named on stderr as
  `<port>: not bumped — plan: declined: …` with the remedy, the cohort
  proceeded with the rest, and the note's candidate row for it now reads
  `proposed, then declined: … — do this one by hand`.
- **A finding:** a silent drop. A member that vanishes between E2's proposal
  and E5's commit is a dependent left broken with nothing said about it.
- **A finding:** every member declining and the verb still committing. With
  nothing to bump there is no cohort, and the verb must decline by name.

**E7. The cohort's own verification.**

```
/tmp/dockhand status
```

- **Worked:** one job on the platform, N+1 runs — the headline and every member
  — and the members build **from source**, because each one's rev+1 names an
  archive that does not exist. That rebuild against the new library is the
  evidence the whole proposal was for.
- **A finding:** a member that installed from a binary archive. Its revision
  did not move, or the archive server has an artifact it should not.
- **A finding:** two jobs. The capacity arithmetic counts environments, and a
  cohort is one build.

**E8. A member that fails.** Break one member's Portfile on the branch and
re-verify.

```
git -C . commit --amend --no-edit          # or a fresh branch; whichever you prefer
/tmp/dockhand verify <branch> --trace
```

- **Worked:** exit **70**, and the message names **the member**, not the
  headline. The headline's own run reads `blocked` with the member blamed, and
  the environment is kept once for the whole guest.
- **A finding:** exit 71 naming the headline. True about the headline and the
  wrong answer to "what happened": the build failed, and the status a caller
  reads has to say so about the port that did it.

**E9. Publish, and read the body.**

```
/tmp/dockhand promote <branch> --no-pr      # then read the body it would send
```

- **Worked:** the pull request body carries the criterion verbatim, the caveat
  under it (`this criterion is necessary and not sufficient…`), the revbumped
  ports each with their **own** link proof (`… links against …`), the
  build-only dependents listed as not revbumped with the reason, the declined
  members, and — if the measurement said so — the `ABI unchanged` refutation.
- **A finding:** a member listed as revbumped with a link proof naming a
  library that did **not** move. The proof is taken against the install names
  the measurement says a dependent can no longer rely on, not against
  everything the headline publishes — a multi-library port has dependents of
  the libraries that stood still, and a line naming one of those under
  "Revision bumped in this change" is evidence for a claim the measurement does
  not support.
- **A finding:** every member reading `; links nothing that moved`. That
  sentence is a measurement and a real answer — the member is build-only in
  fact — but all of them saying it at once means the per-member captures did
  not reach the settlement, and the honest-looking sentence is covering an
  absence.

**E9b. The same body, read one step earlier.** E9's body is read after E7
settled, so the new tip has runs of its own and the header is about them. This
is the window before that, and it needs a **third branch**, because a proposal
is answered once: bump another library, wait for its proposal, then accept it
without asking for the cohort's own verification.

```
/tmp/dockhand bump <a third library> --to <a version whose dylib soname moves>
# wait for the proposal, then:
/tmp/dockhand bump-revision --for <branch> --no-verify
/tmp/dockhand promote <branch> --no-pr      # read the HEADER, not the cohort section
```

- **Worked:** the first sentence reads `this commit adds to a change that was
  verified at `<sha>`, and its own verification has not come back`, and the
  `<sha>` is the tip from E1 — the one the measurement was actually taken on,
  reachable in the branch's history. The cohort section below it is unaffected:
  the criterion, the members, the declined ports.
- **A finding, and the one this step exists for:** the header reading `no
  verification environment on the submitting machine, so nothing was run` on a
  machine that has tart and has just verified the headline. An extended tip
  carries no runs of its own by design and carries an ABI measurement, so that
  sentence sits directly above a claim it contradicts, with no sha offered for
  the reader to check which one is true.
- Clean up before continuing: `/tmp/dockhand discard <branch>`, and
  `git push <fork> --delete <branch>` if the promote pushed it.

**E10. The other answer.** On a second branch, dismiss instead.

```
/tmp/dockhand bump <another library> --to <version>
# wait for the proposal, then:
/tmp/dockhand dismiss <branch>
/tmp/dockhand status
```

- **Worked:** `dismissed on <branch>: dependent-revbump` on stdout, `the
  measurement stands on the note; only the answer to it changed` on stderr, and
  a second `dismiss` of the same branch declines by name. `status` no longer
  prints the proposal line and still prints the `ABI changed:` line — the
  measurement did not stop being true because somebody disagreed about what to
  do with it.
- **A finding:** the finding gone from `status --json`. A dismissal that
  deleted the measurement would propose it again on the next pass.

**E11. Clean up.**

```
/tmp/dockhand cancel <branch> ; /tmp/dockhand discard <branch>
tart list
```

### What part E cannot answer

Whether the proposal was **right**. Every mechanical criterion here is
necessary and never sufficient: an install name and a compatibility version can
sit still while symbols are removed, and a break confined to a header or to a
plugin's own contract leaves no trace in either. E3 is the check on the
roster's completeness and a maintainer's own knowledge is the check on its
correctness — which is why nothing here is ever included on the measurement's
authority alone, and why both answers to a proposal are verbs a person types.

## F. The ABI cohort, end to end, on the fork

Part E probes the mechanism one joint at a time and provokes each refusal.
Part F is the other thing a live check owes: **one uninterrupted run of the
whole feature the overhaul was asked for**, on a real library, with a real VM,
ending in a body a reviewer would read. Nothing here is contrived — no broken
member, no third branch, no amend. If part F does not read like an ordinary
afternoon, the feature is not finished, whatever part E said.

Budget: two guest builds (the headline, then the headline plus its members),
so an evening for a small library and a day for a large one. Do part D first;
F assumes the baseline recipe already works on this machine.

**F0. Preconditions, all of them checkable in one screen.**

```
tart list                                   # the base image is present, no strays
git -C <macports-ports checkout> remote -v  # a fork remote, and it is yours
git -C <macports-ports checkout> status -sb # clean, on the primary branch
ls -l <macports-ports checkout>/PortIndex   # present, and newer than the last pull
/tmp/dockhand --version
```

- **Worked:** every line answers, `PortIndex` exists, the working tree is clean.
- **If not:** `portindex` in the checkout for a missing index; commit or stash
  anything uncommitted — a plan is made against the tip and a dirty worktree is
  a different set of bytes. Do not continue with strays in `tart list`: the
  capacity arithmetic counts environments, and a stray is one of the two slots.

**F1. Choose a library that will actually move, with two or three dependents.**

The whole part turns on picking a port whose next version changes a `dylib`
install name or its compatibility version. Two or three dependents keeps the
second build to an evening and still exercises ordering, the subport collapse
and the build-only listing.

```
cd <macports-ports checkout>
port echo depends:<candidate> | wc -l       # 2-4 is the target
port livecheck <candidate>                  # is there a newer version at all
port -q installed <candidate>               # what this machine has, for comparison
otool -D $(port contents <candidate> | grep '\.dylib$' | head -1)
```

- **Worked:** a candidate with a handful of dependents, a newer upstream
  version, and a versioned install name (`…/libfoo.2.dylib`) — a versioned
  soname is what a soname bump moves.
- **Capture if not:** the candidate list you rejected and why. A port whose
  install name carries no version (`…/libfoo.dylib`) can still narrow its
  compatibility version, but it will not produce the headline clause, and part
  F reads differently. Say which you picked in the report either way.

**F2. `--plan` first. Nothing is written.**

```
/tmp/dockhand bump <library> --to <version> --plan
```

- **Worked:** a plan document on stdout, exit 0, and `git status -sb` still
  clean. The plan names the portdir, the edits, and the predicted delta.
- **A finding:** any file changed on disk. `--plan` is a document and nothing
  else; a plan that wrote is the one thing this flag promises against.
- **Capture if it declines:** the whole decline, verbatim, and
  `/tmp/dockhand bump <library> --to <version> --plan --riders none` to see
  whether a housekeeping rider is what moved. A decline here is a fact about
  the port and usually means a different version, not a bug.

**F3. Mint and verify for real.**

```
/tmp/dockhand bump <library> --to <version>
/tmp/dockhand status
tart list                                   # one guest, and it is dockhand's
```

- **Worked:** a branch `dockhand/<library>-<version>`, exit 0 (submitted) or 60
  (queued), and a run building. `tart list` shows exactly one dockhand worker.
- **Capture if not:** `/tmp/dockhand status --json`, and `tart list` again after
  a minute. A submit that queued is not a failure — 60 says the slots are full.

**F4. Let it finish, and read what the measurement said.**

```
/tmp/dockhand status                        # repeat until the run settles
```

- **Worked:** four things, in this order, under the branch:
  1. `passed (<release>)`
  2. `ABI changed: install name /opt/local/lib/libfoo.2.dylib →
     /opt/local/lib/libfoo.3.dylib, measured between <library>@<old> (binary
     archive) and @<new> (source not recorded) on <release>`
  3. `proposal: N dependents need a revision bump (…) — …`
  4. exit **0**. A proposal is advisory and human-gated.
- **A finding:** `ABI check unavailable: no baseline …`. The archive for the
  version being left was not published for this release, so there was nothing
  to compare against — real, and the run is not usable for part F. Note the
  reason verbatim and go back to F1.
- **A finding:** `ABI unchanged`. The version you picked did not move the
  soname. Also real; back to F1.
- **A finding:** no ABI line at all. Then either the index says nothing depends
  on the library — check with `port echo depends:<library>` — or it could not
  be read, which now says so by name. Neither sentence appearing while
  `port echo depends:` names ports is the failure that matters here.
- **Capture if it fails:** `/tmp/dockhand status --json`, and from it the whole
  `findings` array **and** the run's `manifest` and `baseline` blocks — those
  two are the exact sides the criterion was computed from, and without them a
  wrong sentence cannot be traced to which side was wrong. A passed run hands
  its environment back, so the guest's own copies
  (`/tmp/dockhand-verify/manifest.pre` and `manifest.0`) are only reachable
  while the environment is kept, which is what a *failure* does:
  `/tmp/dockhand shell <branch>` then `cat /tmp/dockhand-verify/manifest.pre`.

**F5. Check the roster by hand. This is the check on the automation.**

```
port echo depends:<library> | sort
/tmp/dockhand status --json | python3 -m json.tool | less    # findings[].candidates
```

- **Worked:** every name `port echo depends:` prints appears as a candidate;
  every proposed one declares the library under `depends_lib` or `depends_run`;
  every `depends_build`-only one is present with `"proposed": false` and the
  reason; every `portdir` is a directory that exists — **for a subport that is
  the parent's directory**, so `php80-Judy` reads `php/php-Judy`.
- **A finding:** a name in `port echo depends:` and not in the candidates. A
  dropped dependency token is a dependent left broken with nothing said about
  it, which is the one outcome this feature exists to prevent.
- **Capture:** the two lists, diffed. `comm -3` between them is the report.

**F6. Accept the proposal.**

```
/tmp/dockhand bump-revision --for dockhand/<library>-<version>
git log --format='%H %s' origin/master..dockhand/<library>-<version>
git show --stat dockhand/<library>-<version>
git show dockhand/<library>-<version> | head -60
```

- **Worked:** the new tip on stdout; **exactly two** commits on the branch; the
  second commit's diff is one `revision` line per member portdir and nothing
  else; its subject is `<library>: revbump N dependents of <library> <version>`;
  its body opens with the criterion from F4 **word for word**, then the caveat
  (`this criterion is necessary and not sufficient…`), then the members with
  their reasons, then the ports examined and not bumped.
- **A finding:** a reworded criterion. One claim, said once, is what lets a
  reviewer check it with `otool` by hand.
- **A finding:** a commit per member. N dependents moving for one reason are
  one logical change.
- **Capture if a member declines:** the stderr line for it and the note's
  candidate row (`status --json`). A decline is correct behaviour — the
  Portfile's shape did not say where a revision line belongs — and the cohort
  proceeding with N−k is the point. A member vanishing with nothing said is not.

**F7. The cohort's own verification.**

```
/tmp/dockhand status
tart list
```

- **Worked:** one job, N+1 runs — the headline first, then each member in
  dependency order — and each member builds **from source**, because its rev+1
  names an archive that does not exist. That rebuild against the new library is
  the evidence the proposal was for. `tart list` shows one worker.
- **A finding:** two jobs. A cohort is one build, in one environment.
- **A finding:** a member that installed from a binary archive; its revision did
  not actually move.
- **Capture if it stalls:** `/tmp/dockhand shell <branch>`, then
  `cat /tmp/dockhand-verify/state ; ls -1 /tmp/dockhand-verify`. The per-subject
  markers say how far the runner got.

**F8. Publish to the fork, without opening a pull request.**

```
/tmp/dockhand promote dockhand/<library>-<version> --no-pr
git ls-remote <fork> 'refs/heads/dockhand/*'
```

- **Worked:** exit 0, the branch on the fork, and no pull request. The machine
  gate does not appear: the proposal was accepted at F6, so there is nothing
  unanswered to hold.
- **A finding:** exit 24. That gate is the machine's and must never meet a
  person at the keyboard — and after F6 there is no open proposal for it to
  hold anyway.
- **Capture if it refuses:** the whole error and `status --json`'s `findings`
  with their `disposition` values.

**F9. Read the body as a reviewer would.** This is the last link and the only
one a reviewer ever sees.

No verb prints the body without publishing it — `--no-pr` pushes the branch and
renders nothing — so there are two ways to read it and they are not equivalent.
The first is the real thing:

```
# (a) in a checkout whose upstream remote is YOUR OWN fork, so the pull
#     request opens against you and nobody else is notified:
git -C . remote -v                          # confirm upstream points at your fork
/tmp/dockhand promote dockhand/<library>-<version>
gh pr view <the url it printed> --json body -q .body | less
gh pr close <the url it printed>            # when you are done reading

# (b) otherwise, check the note the body vouches for, item by item:
/tmp/dockhand status --json | python3 -m json.tool | less
```

Prefer (a). Everything the body says is supposed to be a fact the note already
carries, but *which* facts it selects and how it words them is the half (b)
cannot show — and it is the half a reviewer reads. If you take (b), say so in
the report: F9 was checked against the record and not against the body.

Read for all seven, in order:

1. The per-platform verification lines, in the provider's own words, and
   **`built from source`** only where the run genuinely was.
2. `ABI changed: …` — the criterion from F4, **verbatim**.
3. The caveat directly under it: `this criterion is necessary and not
   sufficient: an install name and a compatibility version can sit still while
   symbols are removed…`.
4. `Revision bumped in this change:` with one line per member, each carrying
   its **own** link proof — `<file> links against <the install name that
   moved>` — or `links nothing that moved` where the sweep found none.
5. `Examined and not bumped:` with the build-only dependents and the reason.
6. Any member the planner declined, as `proposed, then declined: … — do this
   one by hand`.
7. `Branch head <sha>`, the tree date, and the dockhand version.

- **A finding:** a link proof naming a library that did not move. The proof is
  filtered to the names the measurement says a dependent can no longer rely on;
  a multi-library port has dependents of the libraries that stood still, and
  naming one of those under "Revision bumped" is evidence for a claim the
  measurement does not support.
- **A finding:** every member reading `links nothing that moved`. That is a
  real answer for one member and a missing capture for all of them at once.
- **A finding:** `no verification environment on the submitting machine` in the
  header. This tip has runs of its own after F7; the sentence is false and it
  is public.
- **A finding:** the caveat absent. It reaches a reviewer only from the body,
  and the two cases that need it most — a proposal published while still
  proposed, and a dismissed one — write no cohort commit to carry it.
- **Capture:** the whole body, and `status --json` beside it. Every sentence in
  the body is supposed to be a fact the note already carries; a sentence with
  no counterpart in the JSON is the interesting kind of wrong.
- **A finding about this step itself:** that reading the body at all takes a
  pull request. There is no verb that renders it to a terminal, so the one
  artifact a reviewer sees is the one a maintainer cannot preview. Worth
  recording as a gap rather than working around silently.

**F10. Clean up.**

```
/tmp/dockhand cancel dockhand/<library>-<version>
/tmp/dockhand discard dockhand/<library>-<version>
git push <fork> --delete dockhand/<library>-<version>
tart list
git -C . status -sb
```

- **Worked:** no dockhand workers, no branch on the fork, a clean worktree.

### What part F cannot answer

Whether the revision bumps were **necessary**. F proves the chain carried a
measurement from a guest to a reviewer without losing or inventing anything;
it cannot prove the measurement was the right thing to measure. An install name
and a compatibility version can sit still while symbols are removed, and a
break confined to a header or to a plugin's own contract leaves no trace in
either — which is why F6 is a verb a person types, and why F9's caveat is in
the body rather than in this file.
