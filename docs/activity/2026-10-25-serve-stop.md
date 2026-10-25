# serve's stop: what it cancelled unsaid, its unfinished runs said first

2026-10-25. Two lines at serve's stop from v0.5.0's acceptance on rc5,
placed at roadmap 024bbdbf as fixes for main riding v0.6.0.

## serve says nothing of what its own stop cancelled

D-I5 on rc5: at `serve --uninstall`, serve said "could not verify the
GitHub account (… context canceled). Continuing with the current
authentication state; retrying each minute" just before "serve: stopped".
The login gate's look ran on serve's context as the stop cancelled it.

- **Each pass.** engine serve.go's login gate `look`, the follower, the
  outdated scanner, the cleaner, and the submitter return once serve's own
  context is done, saying nothing of the error and changing no state: the
  login's problem, the follower's reported problems, and the scanner's
  stamp stay as they were. The stop's own lines say what stopped.
- **Tests:** TestServeSaysNothingOfWhatItsStopCancelled (engine: a look and
  a refresh on a cancelled context say nothing; GitHub out of reach is
  still said once) and TestServeSaysNothingOfALookItsStopCancelled (serve
  stopped during a look ends on "serve: stopped").

## serve says a run is unfinished before it waits for it

A reboot on rc5: v0.4.0-rc5's serve said "unfinished; the next serve
instance will resume it" and "serve: stopped"; rc5's said neither. serve
said each run's line only as the run returned (`settle`). A Tart check
stops its VM for up to a minute on a context the cancel doesn't reach,
and launchd's SIGKILL likely came first (inferred).

- **Said first.** As serve's stop begins, each run in flight is said
  unfinished, by check number, before serve waits; `settle` doesn't say it
  again. "serve: stopped" follows once the wait ends. `inFlight` keeps
  each run, for its name.
- **Test:** TestServeSaysARunUnfinishedBeforeWaitingForIt, a command
  provider script that ignores the interrupt until released: the line is
  said while serve waits, once, then "serve: stopped".
