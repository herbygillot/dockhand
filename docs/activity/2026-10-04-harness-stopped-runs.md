# 2026-10-04: the runner settles checks a cut serve left stopped

From the M1's quick run at d302e744: B3's `serve --drain` was stopped by the Prime-time harness while the guest still built rust, and its two checks stayed recorded as running for the next serve, as Design v3 §11 has it. H7 read them as left running, a blocker. The Prime-time thread asked that such a cut grade the row, not the sweep.

## What changed

- **The runner settles what a row left stopped**, after the row's act and before the snapshot H7 compares (`settle_stopped` in `lib/common.sh`, called from `run.sh`). Each check `dockhand --json queue` reports as stopped, and that wasn't before the row, is listed in the row's notes and canceled. Where the row's command was cut, by `with_timeout`, which now records what it cut in the row's `cut` file, or by a guard calling `cut_command`, the row is "not run", naming what was cut; with no cut, a check left stopped is the row's failure. H7 then reads a clean queue. It holds for every row, B3 among the twelve that run serve.
- **The self-test** covers both: a cut serve's stopped check makes the row not run, and one left with no cut fails it, each with H7 ok. Its stand-in dockhand learned `cancel`.

## B2 tests the toolchain's reuse

From the M1's run at 464d583c: batch 90's reuse went untested, since the stage's reset before each row deletes the home, database and kept archives with it, so B3's guest built rust and cargo again after B2's had. Keeping dependency archives across resets would carry one row's state into the next, which the reset exists to stop. So where B2 builds (`ACCEPT_B2_BUILD=1`, or the full stage), it checks its bump's branch a second time, `--fresh`, in the same home: the port builds again, and the row fails if the second check's log of the port shows rust or cargo built (`--->  Building rust`), or if the check gave its guest no kept dependency archive.
