# 2026-09-25: the oracle's phase 2, effects refused

Phase 2 of the [oracle](../oracle.md): while a port is evaluated, the
dispatcher refuses writes, the network, and programs that don't only
report, and a port that attempts one is not trusted. Phase 1 put the
dispatcher in observed evaluations only. It is now in every worker.

## What changed

- **The dispatcher is its own script, in every worker.**
  - `dispatcher.tcl` installs it at `worker_init leave` in every
    evaluation, observed or not, before the platform model and
    observation add theirs. Leave traces run in the order they were
    added.
  - Observation (`observation.tcl`) keeps only what it adds on top: the
    declaration traces, the host-read events the tolerance rules judge,
    and `source`'s traces.
- **What is refused:**
  - **writes:**
    - `file mkdir`, `delete`, `copy`, `rename`, `tempfile`, and
      `tempdir`;
    - `file link` given a target;
    - `file attributes` and `mtime` given values;
    - `open` for writing, by mode or by flags;
    - exec output redirected anywhere but `/dev/null` or a channel;
    - Pextlib's `mkdtemp`, `mktemp`, `symlink`, `clonefile`, `lchown`,
      and `xinstall`;
    - `set_pingtime`;
    - the registry's ten writing aliases;
  - **the network:** `socket`, `curl`, the `curlwrap` family, and
    `check_broken_dns`;
  - **processes:**
    - Base's `system`, `mport_exec`, `tracelib`, and
      `macports_create_thread`;
    - a process left running in the background;
    - through `exec` or `open |`, any program not in the table below,
      judged stage by stage along a pipeline, unless it isn't there to
      run (see the survey).
- **The programs that only report.** `macports.HostPrograms` is the
  table, kept in Go beside `ReadOptions` and handed to the dispatcher
  when the interpreter starts. Each entry is a program's base name and
  the form its arguments must take:
  - `any`, for `sw_vers`, `uname`, `getconf`, `echo`, and the like;
  - `only`, where each argument must be one the entry lists: xcrun
    finding a tool or an SDK but not running one, `java_home` without
    `--exec`, `sysctl` reading, compilers and interpreters asked their
    version or search paths but never given code;
  - `wrapper`, for `env`, whose command is judged in turn;
  - `subcommand`, for `git`'s `rev-parse`, `status`, `log`, and
    `describe`, which Base's `source_date_epoch` runs, never with
    `--output`.

  The 2026-09-23 host inventory lists every program run while the tree
  was evaluated: `java_home`, `xcrun`, `echo`, `git`, `sysctl`, the
  compilers' `--print-search-dirs`, `machine`, `env`, `uname`, `rustc`,
  `perl -V:`, and `ruby -e`. All are admitted but the last, which runs
  code. The rest of the table is the toolchain queries beside them. The
  hook grammar still refuses `exec` outright, and the table is there for
  it when it admits programs.
- **A refusal can't be caught away.**
  - The refused command raises an error in the worker (`DOCKHAND
    REFUSED`) and is recorded in the parent interpreter, out of the
    Portfile's reach. A `catch` hides the error, not the record, and a
    failed `mportopen` deletes the worker, not the record.
  - `metadata` then answers with the refusals alone. Go turns them into
    `macports.ErrRefused`, which names each command, why it was refused,
    and where. `assess` reports it as `effect-refused`.
- **Two things learned about MacPorts' workers:**
  - Pextlib, with `system`, `curl`, `symlink`, and the rest, is loaded
    by `PortSystem`, after `worker_init`. So the dispatcher hides what
    exists at `worker_init` and the rest when `PortSystem` returns.
  - Base's `catch` is a procedure of its own (`signalcatch.tcl`). So
    where a refusal happened is the innermost frame in the ports tree, a
    Portfile or PortGroup, before any other file.

## Tests

- `TestTheDispatcherRefusesEffects` covers each kind of refusal,
  observed and not. The file it would have written doesn't exist, and
  the one it would have changed is unchanged. The refusal names the
  Portfile line, even when the Portfile caught it.
- `TestTheDispatcherAdmitsProgramsThatOnlyReport`: `uname`, `sysctl
  -n`, `sw_vers`, `env` wrapping a listed program, a pipeline of listed
  programs, output to `/dev/null` or to a channel, reading a file,
  reading a link, and a caught `exec` of a program that isn't installed
  all pass.
- `TestHostProgramsAreWellFormed` and `TestHostProgramsReadBackAsTcl`
  check the table itself.
- The Tcl-level observation tests build their worker the way
  `worker_init`'s traces do, the dispatcher first.

## What it doesn't do

Isolation is best effort, as settled. A Portfile that goes around the
dispatcher on purpose still can: `interp invokehidden`, a child
interpreter of its own, or C code. The parent's aliases, such as
`get_tool_path`, still do their host reads in the parent. The comparison
with a fresh Tart guest that the scope put in phase 2 needs the
evaluator in a guest, which the v3 Tart provider brings, so it waits for
that.

## The survey

The whole tree at `abd9fff84df`, 41,790 ports with MacPorts 2.12.6 from
`/opt/macports-test`, compared with phase 1's survey
(`~/.dockhand/surveys/2026-09-25-phase2-abd9fff.jsonl`):

- **One port moved: rb-rttool, input-found to unknown.** Its Portfile
  reads `${ruby.lib}` at parse time. The ruby PortGroup then runs
  `${prefix}/bin/ruby1.8 -e {require 'rbconfig';…}` in a `catch` that
  falls back to a default. ruby1.8 isn't installed on this Mac, so in
  phase 1 the `exec` failed and the default was used. Phase 2 refused it
  first.
- **So a program that isn't there is not refused.** An absolute path
  that isn't executable runs nothing, so `exec` is let fail as it would
  have, and the Portfile falls back. With ruby1.8 installed, the call is
  still refused, since `-e` runs code. Phase 4's empty installation will
  leave the prefix's ruby absent in every context.
- **The final code matches phase 1 on every port.** Everything changed
  after the survey only relaxes refusals: the missing program, `file
  link` reading a link, and refused calls no longer counted in a ledger
  no one reads. The whole tree had exactly one refusal. Surveying the
  ruby category again (1,378 ports) with the final code brings
  rb-rttool back to input-found, with no other change.
- **The cost is unchanged.**

  | Survey | CPU seconds | Wall |
  |---|---|---|
  | baseline | 32,003 | 67.1 min |
  | phase 1 | 34,903 | 63.8 min |
  | phase 2 | 32,104 | 67.8 min |

  The runs differ by their load more than by their code.

Phase 2's acceptance was that each newly inconclusive port attempted a
refused effect. The one that did attempted one that could not take
place, so the rule was corrected. At this commit no port in the tree
attempts a write, the network, or a program outside the table while it
is evaluated. The refusals guard edits, not the tree as it is.
