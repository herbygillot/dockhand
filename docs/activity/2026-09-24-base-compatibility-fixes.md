# 2026-09-24: Base compatibility fixes, and tests that choose their Base

Step 1 of the roadmap's Next, the Base compatibility item; step 2 of the
[Base design](../macports-base-design-prospective.md)'s order of work,
from the baseline recorded in the [independent
review](../reviews/2026-09-24-macports-base-independent-review.md). With
the startup fix already in (`2026-09-24-base-version-gate.md`), these are
the other differences that failed tests on Base master (2.12.99, Tcl 9).

## The fixes

- **The fetch target's code, loaded where master defers it.** Master
  defines the fetch procedure, `checkfiles`, and `assemble_url` only in
  `portfetch_run`, which `portutil::target_load` requires, as Base's own
  fetch does first (`portfetch.tcl:142`). `load_fetch_target`
  (`eval/compatibility.tcl`) does the same before the fetch details and
  the native fetch plan; on 2.12, which has no `target_load`, it does
  nothing. Master was reporting "fetch procedure is unavailable" and
  `invalid command name "portfetch::checkfiles"`.
- **The livecheck copy's `glob`.** Tcl 9 returns nothing where 8.6 raised
  an error on no match, which the copy relied on to leave a tree without
  checker definitions with no effective livecheck. It now uses
  `glob -nocomplain` and raises on an empty list itself
  (`eval/evaluator.tcl`); Base's own guard (`portlivecheck_run.tcl:101`)
  has the same Tcl 9 problem upstream.
- **The livecheck differential test** reads master's
  `portlivecheck_run.tcl`, whose loop reads `$tempfd` and leaves it
  open, as well as 2.12's `portlivecheck.tcl`.
- **The RPC layer's encodings** (`tcl/rpc/loop.tcl`, `session.go`). Tcl 9
  refuses to decode invalid UTF-8 or to encode a lone surrogate, and both
  conversions ran outside a `catch`, so either ended the session. The
  loop now reads every argument before refusing an undecodable one, so
  the stream stays framed, and turns an unencodable reply into the call's
  error. Go refuses arguments that are not UTF-8 before sending them and
  replies that are not UTF-8 (Tcl 8.6 writes the surrogate that way), as
  call errors that leave the session usable. `DOCKHAND_TEST_TCLSH` runs
  the protocol tests on another `tclsh`; both new tests fail on Tcl 9
  without the change.

## Tests choose their Base

Tests found `port-tclsh` and `portindex` on `PATH`. On this Mac the first
`PATH` entry is `/opt/macports-test/bin`, another 2.12.6 installation, so
the suite had been testing that one rather than `/opt/local`. Every
MacPorts test now takes its installation from
`DOCKHAND_TEST_MACPORTS_TCLSH` alone, and `portindex` from beside it
(`testsupport.MacPortsTclsh`, `MacPortsTool`), and skips without it. The
CLI tests' configuration names the prefix too, so the product's own
`portindex` lookup never falls back to `PATH`. Test evaluators take
`DOCKHAND_TEST_BASE_ADAPTER` (`testsupport.BaseAdapter`). The whole suite
passes with the variable set and no MacPorts program on `PATH`, and with
it unset.

## The parent-side gap, pinned

Master asks compiler and SDK questions in its parent interpreter
(`portlib.tcl`), where the worker's observation cannot see them, so ports
that read the host look host-independent there. Two regressions pin what
2.12.6 sees: `TestCompilerDependentFetchInputReadsTheHost`, a synthetic
port reading `configure.cxx` with `use_xcode yes` as gpsd does, and, with
`DOCKHAND_TEST_PORTS_TREE` naming a checkout,
`TestPortsThatReadTheHostThroughBaseCompilerQueries` over the eleven
ports the baseline found (qt4-mac, qt64-qtwebengine and its docs,
poedit, godot, openjdk8, gcc48, gcc49, gcc8, libgcc8, gpsd) in a modeled
Darwin 22 x86_64 context. All pass on 2.12.6 and all fail on master; under
the preview adapter they skip with the reason, until the adapter closes
the gap (Base design, step 5). Preparation on master stays refused by the
version gate meanwhile.

With these, every `internal/macports` package, `workflow/preparation`,
`upstream`, and `outdated` pass against master with the preview adapter.
