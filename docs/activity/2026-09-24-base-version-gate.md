# 2026-09-24: the evaluator checks vercmp after mportinit and gates Base by version

Step 1 of the roadmap's Next, its first item; the design is the
[Base design](../macports-base-design-prospective.md), order of work,
step 1.

## The startup check

`check_startup` required `::vercmp` before `mportinit`. Base 2.12 loads
Pextlib, and `vercmp` with it, when the `macports` package loads; master
(e545ebe8c) loads it only in `mportinit`, so the evaluator refused master
before ever initializing it. The check is now two: `check_startup` asks
for the interfaces the package provides (`mportinit`, `mportopen`,
`mportinfo`, `mportclose`, `ditem_key`), and `check_initialized` asks
for a working `vercmp` once `mportinit` has run
(`internal/macports/eval/compatibility.tcl`).

## The version gate

That failed check was the only thing stopping Base master from
preparing, and master asks the host compiler and SDK questions from its
parent interpreter, where dockhand does not observe them; the baseline
found eleven ports whose preparation would rest on answers dockhand
never saw. So the fix ships with a gate. A new `probe` RPC loads the
package and returns `macports::version` before `mportinit` reads the
host's configuration, and `admitBase` (`compatibility.go`) judges it:

- released 2.11.x and 2.12.x are admitted;
- a development build (patch 90 or above; master reports x.y.99) is
  refused, unless `Evaluator.Adapter` is `preview`;
- `preview` on a release is refused, as is any other adapter name;
- an older family is refused with a pointer to `sudo port selfupdate`;
- a newer family is refused as not supported yet, saying status and
  recorded evidence remain usable;
- anything else is an unrecognized version.

No command sets `Adapter`. The evaluator's tests read
`DOCKHAND_TEST_BASE_ADAPTER`, so the suite can run against master; the
product's `DOCKHAND_BASE_ADAPTER` waits for the preview adapter.

2.10 and older were admitted before, on capability checks alone, and are
refused now; `docs/macports-compatibility.md` says so and records master.

## Tests

`TestStartupChecksCapabilitiesWithoutVersionGate` asserted that a Base
reporting 99.0 passed; it is replaced by `TestBaseVersionGate` (the
decisions above), `TestStartupChecksSplitAroundInitialization` (the
startup checks pass without `vercmp`, the check after `mportinit` fails
without it or with a broken one), and
`TestProbeReadsVersionBeforeInitialization`. Against
`/opt/macports-master` (2.12.99, Tcl 9.0.4), inspection is refused
without the preview adapter and passes with it; the package passes on
2.12.6.
