# Throughput's remaining levers

The last item of the reconciled queue: the two levers left after the 2026-09-17 throughput work, once the structural items had given the first one a home.

## One session per preparation

Every native evaluation of a preparation started its own MacPorts interpreter: the baseline in `load`, each version-probe candidate, each candidate edit, the final edit. The edit's input now opens one interpreter session on first use, bound to its workspace tree, and every native evaluation goes through it: the baseline, the candidates, the final edit, and the version probe's batched evaluations, which used to open a session of their own per call. The session closes with the input; `Prepare` closes it on return, and a `VersionProbe` has `Close`, which assess, outdated, and the preparation adapter call when they are done with the probe.

Modeled observations never touch the session. Each still starts its own interpreter, because observation setup changes the interpreter it runs in, the concern raised on 2026-09-17 about reusing a context across ports.

The guard is a differential test in `eval`: the fixture tree, and forty Portfiles sampled from a ports tree named by `DOCKHAND_GUARD_TREE`, are each evaluated through a fresh interpreter and through one session shared across the whole sample, in the same order, and the snapshots must be identical, whole-Portfile and selected-only alike. Against the pinned corpus worktree the forty compare equal.

## What it bought

| Measure | Before | After |
|---|---:|---:|
| `portedit` tests, summed elapsed | 153.4s | 118.1s |
| eleven-port family sample, `assess` | 8.0s | 7.3s |
| `bump R-jsonlite --diff` | 29.7s | 28.8s |

A bump preview is bounded by things the session does not touch: materializing the tree, the modeled observations, and the download. The gain is in what evaluates natively many times, which is the test suite and version probing.

## Seams for operands

`TestNativePlatformOperands` prepared a port five times end to end, once per form a Darwin comparison takes; `TestPrepareMultilineOSBoundary` prepared one more. The forms are recognized by `scanPlatformNeeds`, a pure scan of the Portfile text, and that is where they are now tested: scalar, inverted, option, alias, formatting, derived, multiline, literal, architecture, and conjunction, plus three refusals, in one table that runs in milliseconds. Two operand cases and one boundary case remain end to end. Regeneration already sat at its seam: the `dependency` package tests the block layouts, the generators' requirements, and the manifest ownership, and `portedit` keeps one end-to-end case for a missing helper and one for the module-mode toolchain raise; nothing was moved.

## Tests

`eval`: the session guard. `portedit`: the operand-form table; the remaining end-to-end cases pass through the shared session; the suite's summed time is the number above. The whole suite passes and `make deadcode` is clean.
