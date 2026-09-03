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

---

## A. The control run — one port must not have moved

**A1. Make a one-portdir branch by hand.** Any small, quick port you can
build; `textproc/oniguruma` is used below because part B needs it anyway.

```
cd <macports-ports checkout>
git switch -c live/solo-control origin/master
printf '\n' >> textproc/oniguruma/Portfile      # any no-op edit; the diff only has to touch the portdir
git commit -am 'oniguruma: no-op, live check'
```

**A2. Verify it.**

```
/tmp/dockhand verify live/solo-control
```

- **Worked:** one line, naming one port and one release — `verify: submitted
  oniguruma on <Release> (job dockhand-worker-…)`.
- **A finding:** the line naming two ports, or two jobs starting.

**A3. While it is still building, look at what the guest was told to do.**

```
/tmp/dockhand shell live/solo-control
# inside the guest:
ls -1 /tmp/dockhand-verify
cat /tmp/dockhand-verify/argv
cat /tmp/dockhand-verify/argv.lint
exit
```

- **Worked:** `ls` lists **exactly** `argv`, `argv.lint`, `log`, `state` —
  nothing else. `argv` holds four lines, `-d`, `-N`, `install`, `oniguruma`.
  `argv.lint` holds two, `lint` and `oniguruma`.
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
- **A finding:** a line reading `oniguruma: passed (<Release>)`. That is the
  cohort rendering applied to one subject, and it moves every status golden.

**A5. Clean up before part B.**

```
/tmp/dockhand discard live/solo-control
```

---

## B. The cohort that passes

**B1. Make a two-portdir branch by hand.** A library and a dependent that
would need a revision bump when it moves — the shape the cohort exists for.

```
cd <macports-ports checkout>
git switch -c live/cohort-pass origin/master
# the library: any no-op edit is enough to make the portdir part of the change
printf '\n' >> textproc/oniguruma/Portfile
# the dependent: bump its revision by hand
$EDITOR sysutils/jq/Portfile                     # revision 0 -> revision 1, or add "revision 1"
git commit -am 'oniguruma, jq: live cohort check'
git show --stat HEAD
```

- **Worked:** `git show --stat` names two files under two different
  `<category>/<port>` directories.
- **A finding:** nothing here — this is your own commit. But if both files are
  under one portdir, the branch is not a cohort and part B proves nothing.

**B2. Verify the branch.**

```
/tmp/dockhand verify live/cohort-pass
```

- **Worked:** one line naming **both** ports and **one** job:
  `verify: submitted jq, oniguruma on <Release> (job dockhand-worker-…)`. The
  order is alphabetical by portdir (`sysutils/jq` before
  `textproc/oniguruma`), which is deterministic and deliberately arbitrary.
- **A finding:** two jobs, two lines, or the old refusal — `one at a time for
  now`, or any message about the branch changing more than one portdir. That
  refusal was retired in S11; seeing it means the binary under test is not the
  one you built.

**B3. Look at the note while it builds.**

```
/tmp/dockhand status
```

- **Worked:** the branch shows **two** lines, one per member, each naming its
  port: `jq: running (<Release>)` and `oniguruma: running (<Release>)`. One
  job, one environment, two runs.
- **A finding:** one line for two members; two environments in `tart list` for
  one branch; a run keyed by release alone.

**B4. Confirm the guest received a cohort instruction set.** This is the step
nothing offline can do.

```
/tmp/dockhand shell live/cohort-pass
# inside the guest:
ls -1 /tmp/dockhand-verify
cat /tmp/dockhand-verify/subject.0
cat /tmp/dockhand-verify/argv.0
cat /tmp/dockhand-verify/argv.1
cat /tmp/dockhand-verify/state.0     # only once the first member has finished
exit
```

- **Worked:** `ls` lists `subject.0`, `argv.0`, `argv.0.lint`, `subject.1`,
  `argv.1`, `argv.1.lint`, `log`, `state`, and `state.<i>` for each member that
  has finished. `subject.0` holds exactly `===> dockhand subject: jq`.
  `argv.0` holds `-d`, `-N`, `install`, `jq`; `argv.1` the same for
  `oniguruma`. No bare `argv` and no bare `argv.lint`.
- **A finding:** a bare `argv` beside the numbered ones (two instruction sets,
  one guest); a marker file holding anything but one line; a `-s` in an argv
  nobody asked to build from source.

**B5. Let it finish, then read the verdicts.**

```
/tmp/dockhand status
/tmp/dockhand log live/cohort-pass | grep -n 'dockhand subject:'
tart list
```

- **Worked:** both members read `passed (<Release>)`; the marker lines appear
  once each, in build order, `jq` before `oniguruma`; `tart list` shows no
  `dockhand-worker-*` — one guest, released once, after **both** members were
  terminal.
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
/tmp/dockhand discard live/cohort-pass
```

---

## C. The cohort that breaks

This is the part the synthesized corpus is least able to stand in for: what
one guest does when a member fails halfway through, and which environment
survives.

**C1. Make the same branch with the FIRST member deliberately broken.** First
in build order, so the second is never reached.

```
cd <macports-ports checkout>
git switch -c live/cohort-fail origin/master
$EDITOR sysutils/jq/Portfile      # add a line: build.cmd false
printf '\n' >> textproc/oniguruma/Portfile
git commit -am 'jq, oniguruma: live cohort failure check'
```

`build.cmd false` fails in the build phase and produces MacPorts' own
`Error: Failed to build jq: command execution failed`, which is the exact
shape the judge reads.

**C2. Verify, and wait for it.**

```
/tmp/dockhand verify live/cohort-fail
/tmp/dockhand status
```

- **Worked:** the note settles to
  - `jq: failed (<Release>) — environment kept: dockhand-worker-… — Failed to
    build jq: command execution failed`
  - `oniguruma: blocked (<Release>) — jq fails to build; this member is
    untested`

  The failing member owns the failure; the member the runner never reached is
  blocked, blamed on a sibling, and says so in those words — "untested", not
  "failed". Two verdicts, one sentence about each.
- **A finding, and the one worth the most:** `oniguruma` reading `passed`
  (a member that was never built recorded as evidence); `oniguruma` reading
  `failed` (a member disproven by a sibling's breakage); either member blocked
  on a *dependency* sentence — `dependency jq fails to build; the change
  itself is untested` — which means the roster match failed and this change's
  own breakage was read as a stranger's.

**C3. Confirm the environment is kept exactly once.**

```
tart list
/tmp/dockhand shell live/cohort-fail
# inside: cat /tmp/dockhand-verify/state ; cat /tmp/dockhand-verify/state.0 ; ls -1 /tmp/dockhand-verify
exit
```

- **Worked:** exactly **one** `dockhand-worker-*`, and the shell lands in it.
  `state` says `failed`; `state.0` says `failed`; there is no `state.1` and no
  `subject.1` output in the log, because the runner broke before the second
  member.
- **A finding:** no worker (the failure's debug environment was handed back);
  two workers for one branch; a `state.1` file, which would mean the runner
  kept going after a member failed.

**C4. Confirm the log's attribution.**

```
/tmp/dockhand log live/cohort-fail | grep -n 'dockhand subject:\|^Error:'
```

- **Worked:** one marker, `===> dockhand subject: jq`, and the `Error: Failed
  to build jq` line after it. No marker for `oniguruma`.
- **A finding:** a marker for a member the runner never built, or a marker
  printed after the failure.

**C5. Confirm the gate refuses to publish it.**

```
/tmp/dockhand promote live/cohort-fail
```

- **Worked:** it refuses, and says the verification failed. Do **not** pass
  `--no-verify`.
- **A finding:** a pull request being opened.

**C6. Clean up.**

```
/tmp/dockhand cancel live/cohort-fail     # releases the kept environment
/tmp/dockhand discard live/cohort-fail    # removes the branch and its note
tart list                                 # expect: no dockhand-worker-*
git -C <macports-ports checkout> switch master
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
- **Build order is alphabetical by portdir, not topological.** In part B, `jq`
  is built before `oniguruma` because `sysutils` sorts before `textproc`, and
  the dependent going first is decided by a category name. It is deterministic
  and blame does not depend on it — the judge matches the log's name against
  the roster, not against a position — but ordering members by declared
  dependency is a real improvement waiting for a step of its own.
