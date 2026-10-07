# 2026-10-07: the runner's cancel test, hardened

`TestACancelIsAppliedByWhoeverHoldsTheRun` was asked after as flaky. It didn't fail here: 400 runs alone, and 600 under `-race` with the engine and command suites running beside it as load, all passed, and no CI failure in the last hundred runs is this test; its one known failure, on Intel at 404ac160, was fixed by 2f0dce70, which asserts who applied the cancel rather than when.

Two hazards were left in it, and both turn a slow or failing run into a hang rather than a failure:

- **Drive's error was checked inside its goroutine.** A `require` there can't stop the test, so a failed Drive never sent its result, and the test blocked on it until the package's ten-minute timeout, which reads as a flaky timeout with no cause. Drive's run and error now come back over a buffered channel and are judged in the test, and the wait for them is bounded, failing with what it waited for.
- **The provider had five seconds to be given the job.** On CI's Intel runner, where engine tests have taken 17 seconds, that could run out before the check began. The bound is two minutes (`settleWait`), and it ends as soon as the job arrives, so a fast run is no slower.

Nothing in dockhand changed.
