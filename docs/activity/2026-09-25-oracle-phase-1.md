# 2026-09-25: the oracle's phase 1, the dispatcher in shadow mode

Phase 1 of the [oracle](../oracle.md): the host-facing commands reach the
host only through a dispatcher, which passes every answer through
unchanged, records where it came from, and judges nothing new. It
replaces the execution traces in `observation.tcl`.

## What changed

- **Hidden and dispatched.** In each worker, at `worker_init leave` (not
  at `PortSystem`, where the traces began), `exec`, `file`, `open`, and
  `glob` are hidden with `interp hide` and aliased to
  `::dockhand_observation::dispatch`. It calls the hidden command with
  `interp invokehidden`, so the answer is the host's, as before.
- **The ledger.** Every call is counted by command, `file` subcommand,
  and source:
  - `pure`, path arithmetic such as `file join`;
  - `tree`, inside the captured tree or its base;
  - `base`, MacPorts Base's own library, the `port1.0` its workers load;
  - `process`, `exec` or `open |`;
  - `relative`;
  - `enumeration`, `glob`;
  - `host`.

  The prefix test that classifies a call costs nothing measurable. The
  ledger returns to Go as the seventh field of `observation_details`,
  `macports.PortObservation.Ledger`. `eval.Evaluator.Ledger`, when set,
  hears each observed port's ledger, and `tools/survey -ledger <file>`
  writes them, one JSON line per port and context. In apache-ant's
  native context, for instance, what reaches the host is:

  - 5 `file executable`, 4 `file exists`, and 1 `file isdirectory`;
  - the Java PortGroup's 2 `exec` of `java_home`;
  - 1 `glob`.

  Everything else is Base's own library (47 `source`), the tree, or
  path arithmetic (84 `file join`).
- **The same events, walked only when needed.** A host read becomes a
  `dockhand.host-access` event under the old rules: after `PortSystem`,
  owned by the Portfile, and for `file` only the subcommands that read
  the filesystem. The events carry the same problem strings, so
  `observe.Tolerate` judges exactly as before. The frames are walked only
  after the read is known to reach outside the tree, since the frame walk
  was what made the traces cost 28%.
- **`source` stays traced.** A script run by a hidden `source` loses its
  frames to `info frame`. `source` makes about fifty calls a port, so its
  enter and leave traces feed the ledger and the events.
- **Ownership is explicit.** The old rule, "a Portfile frame on the
  stack", worked by accident. A read made while a Portfile line's
  arguments are substituted, such as Base's compiler probe under
  `${configure.cxx}`, has no frame for the line in compiled code. The
  traces saw one only because tracing a command stops Tcl compiling it
  inline, which was also part of their cost. The dispatcher counts the
  Portfiles being sourced (from `source`'s traces). A read is the
  Portfile's while one is, or when a Portfile frame is on the stack,
  which covers procedures a Portfile defined and that run later.
  `TestCompilerDependentFetchInputReadsTheHost` found this: under the
  frame rule alone the dispatcher saw the compiler probe and dropped it.

## Tests

- The evaluator's live tests pass against MacPorts 2.12.6
  (`DOCKHAND_TEST_MACPORTS_TCLSH`), including
  `TestObservationTraceRecordsPortfileHostReadsWithFrames`, which pins the
  events' problem strings, order, and frames. So do the rest of
  `internal/macports/...`, with and without the live evaluator.
- `TestTheDispatcherCountsEveryCallByItsSource` observes a Portfile that
  reads a file in its tree, a host file, joins a path, and runs a
  process. The ledger counts each under its source, and the host read is
  still an event.

## The survey

The whole tree at `abd9fff84df`, with the same MacPorts
(`/opt/macports-test`, 2.12.6), against this session's baseline:

- **41,777 of 41,790 ports unchanged.**
- **13 moved from input-found to unknown** (fetch, probe-inconclusive):
  AppKiDo, FFView, cryptlib, ctop, gdchart, socket_vmnet, syft, and
  ffmpeg, ffmpeg-devel, ffmpeg4, ffmpeg6, ffmpeg7, ffmpeg8.

Each of the 13 reads a value Base computes on first use while a Portfile
line's words are substituted, the case the old frame rule couldn't see:

- **`${source_date_epoch}`** (ctop, syft) runs `git rev-parse`,
  `git status`, and `git log` in `portmain::get_source_date_epoch`.
- **`set CFLAGS ${configure.cflags}`** at the top level (gdchart) runs
  compiler selection, which probes the host's compilers.
- **`configure.sdkroot`** (AppKiDo and others) looks for SDKs on disk.

They are host reads the traces missed, not mistakes of the dispatcher.
Their answers come later: `source_date_epoch` from the captured commit's
time rather than from running git (phase 2 or 6), and the compiler and
SDK from the facts table (phase 5).

## The cost

The whole-tree runs differed by 9.1% in CPU time, but they ran under
different loads: the baseline shared the Mac with probe VMs and test
suites. A controlled comparison ran the old traces, the dispatcher, and
the old traces again, back to back, over `textproc` and `sysutils`
(3,319 ports):

| Run | Wall | User | System | CPU |
|---|---|---|---|---|
| old traces | 513 s | 1,116 s | 1,417 s | 2,533 s |
| dispatcher | 511 s | 1,119 s | 1,417 s | 2,536 s |
| old traces again | 515 s | 1,120 s | 1,426 s | 2,546 s |

The dispatcher costs what the traces did, within the runs' own spread of
half a percent. Its two extra unknowns are ctop and syft, two of the 13.

## Acceptance

The scope's test for phase 1 was that the survey match the baseline.
It matches except for the 13 ports the old traces missed. Matching them
would mean reproducing a Tcl compilation quirk, so they are taken as a
correction, recorded here for the person to reverse if they disagree.
The phase-1 survey (`2026-09-25-phase1-abd9fff.jsonl`) is the baseline
phase 2 is measured against. The cost test is met: the dispatcher costs
what the traces did.

The person settled isolation as best effort the same day. There is no
kernel sandbox, since `sandbox-exec` is deprecated and the rule is
official interfaces only. The dispatcher answers and refuses, phase 6
gives evaluation its own configuration, and a fresh Tart guest is an
occasional check, not a gate.
