# The cycle observes open pull requests

Proposed by the user on 2026-09-17: rather than asking for `refresh` by hand, a processing cycle should look at open contributions' PRs on a best-effort basis, since the person may be offline.

Every whole-repository cycle, which is what `status` on a terminal and `start` run, now calls `observePullRequests`. It lists the open contributions that have a recorded PR and, for each one not looked at within `PullRequestInterval` (five minutes by default), does exactly what `refresh` does through the shared `refreshChange`: it records the PR's state and, while open, its mergeability, review, and checks; a merged or closed PR retires the contribution and cleans its local and fork branches under the existing guards. At most four PRs are looked at per cycle, so a repository with many open contributions spreads its forge calls over a few cycles.

A failed observation, whether the forge is unreachable, the credential is rejected, or the PR does not match, leaves the last recorded observation in place and is reported only at `-v`; the next look waits a full interval, so being offline costs one quiet attempt every five minutes and changes nothing. Retirement, the one outcome that acts, is reported at the info level with the branches it cleaned. The throttle is per process, keyed by repository and change, so a fresh `status` looks once at everything open and then settles into the interval.

`refresh` remains for "look now" and for closed or historical contributions, which the cycle does not touch.
