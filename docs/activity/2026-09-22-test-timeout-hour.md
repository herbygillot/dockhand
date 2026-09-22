# 2026-09-22: the default test timeout is an hour

The git-devel bump to 2.56.0-rc2, run to exercise the day's code, verified
and published ([#34835](https://github.com/macports/macports-ports/pull/34835))
with its test step recorded as an advisory failure: "timed out after
1800s". Git ships 1,059 test scripts and runs them serially; in thirty
minutes the Tahoe guest got through 657 of them with no real failure,
every "not ok" being one of git's own marked known breakages. The
timeout did what it is for, stopping a phase that would otherwise hold
the VM, and the PR body said so rather than claiming the tests ran, but
it cut short a suite that was finishing, not hanging.

`DefaultTestTimeout` is an hour now. Nothing else bounds the phase: the
workflow's build waits are unbounded, and the guest's runner has no cap
of its own, so a suite that needs fifty minutes gets them, and one that
hangs is stopped at sixty and recorded as before. `--test-timeout` still
sets it per run, and the CLI help reads the default from the constant.
The rc1 verification on the 19th, which reached t7063-status-untracked-
cache and failed it, had forty-five minutes; whether that script fails
on rc2 is still unrecorded, and the next declared-policy run of the
port will say.
