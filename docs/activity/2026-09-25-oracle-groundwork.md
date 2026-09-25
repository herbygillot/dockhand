# 2026-09-25: groundwork for the oracle

What the oracle ([scope](../oracle.md), roadmap step 8) needs before its
first phase, gathered on the Mac.

## The survey, back on main

Every oracle phase is proven by a whole-tree survey compared with the one
before. That was v2's `dockhand assess --all`, deleted with v2's command
line, and a `v2-final` binary can't stand in, since it lacks the
evaluator changes the oracle will make on `main`. `tools/survey` brings
it back as a developer tool beside `tools/stateperf`
([README](../../tools/survey/README.md)):

- **The same survey.** It wires `internal/assess` as v2's `app.Assess`
  did: MacPorts' evaluator through a staged, offline port index, and the
  go2port and cargo2port helpers.
- **The same journal**, so the old baselines compare directly, resumable,
  plus a `<journal>.run.json` of how the run was made and what it cost.
- **Comparison built in.** `-compare` replaces the ad-hoc Python used for
  the 2026-09-22 comparisons. It covers outcome moves, regressions,
  changed findings by pattern, fetch guards lost or changed, fetches
  refused then accepted, Portfiles fully covered, and where ports stop.
  Run on the 09-22 rerun and effect-rule journals, it reproduces that
  evening's figures: 176 unsupported to input-found, 78 to unknown, 272
  fetches refused then accepted.

**Parity.** `v2-final`'s `assess --category textproc` and
`tools/survey -category textproc` ran on the same commit (`abd9fff84df`),
MacPorts (`/opt/macports-test`, 2.12.6), and index cache. Their journals
match port for port on all 1,960 ports: outcomes, findings, fetch guards,
and Portfiles covered, with no moves.

**Which MacPorts.** The earlier baselines were run without `-p`, so they
evaluated with the first `port-tclsh` on PATH, `/opt/macports-test`'s, as
the host inventory's paths show. New baselines pin it with `-prefix`.

## Corrections to the scope

`docs/oracle.md` named `platform.tcl` as the platform overrides and gave
the hook grammar a program list; neither holds. The overrides are
`override_vars` in `observation.tcl` and `evaluator.tcl`, and
`platform.tcl` is the operand capture profiles need. The grammar refuses
`exec` and `system` outright and admits commands by table, so phase 2's
program allowlist is a new table for both, not one taken over.

## Kept

The instrumented evaluator behind the 2026-09-23 host inventory existed
only in `/tmp/dockhand-inventory`. It is now beside its data, in
`~/.dockhand/surveys/2026-09-23-host-inventory/instrumentation/`.
