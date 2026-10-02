# 2026-10-02: serve's tests ran out of time in CI after batch 49

CI failed on main from de88e1f0, batch 49, and on #2, which changed only docs: `TestServeCleansUpAfterAMergeOnceADay` each time, and once with `TestServeWorksThroughYourOutdatedPorts` and `TestServeSubmitsNoMoreThanTheDailyLimit`, all "Condition never satisfied".

## Why

The command line's serve and watch tests wait for what a running serve says with `require.Eventually`, bounded at five or ten seconds. Batch 49 ran engine's tests in parallel and the command line's as six processes, beside the other packages; on CI's runner, three cores, that's far more work at once than before, and what serve says arrived after the bound. On this Mac's 18 cores the same tests finish in a second or two, which is why the bound held here.

## What changed

- The waits share one bound, `settle`, two minutes. It's a bound, not a pace: each wait ends as soon as the words appear, so a passing run is no slower.
- `make test` runs one command shard for each three cores, at most six: six on this Mac, one on CI's runner.

## Verification

- Six copies of the serve and watch tests at once beside sixteen busy processes on this Mac, all passing; `make test`; CI on the push.
