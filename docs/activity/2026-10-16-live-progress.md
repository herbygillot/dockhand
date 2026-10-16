# v0.4.0 part 3: live progress from the guest

- The Tart guest program says the step of its build under way: `mark`,
  which records where each step begins in the log, sets `phase` and writes
  the results at once, beside `building`; both clear between targets.
  The results gain an optional `phase` field, read where present, so the
  protocol stays 1: the guest program is the host's own, shipped each run.
- Tart's follow says "building jq (2 of 5): fetch" as a progress line
  whenever what the guest builds or its step changes, and only then: at
  most a line a read (every 10 s) and one a step, which is SQL-S4's
  coalescing for this source of progress.
- `store.Reader.LatestRunEvent(run, kind)`: a run's newest event of a kind,
  one query. `engine.Queue` reads each running check's newest progress
  with it (`QueuedCheck.Progress`), and status's list of checks shows it
  in the DETAIL column (`progress` in the JSON).
- `status --watch` redraws for progress at most every 5 seconds, a
  pending line kept for the next redraw rather than dropped; state-changing
  events still redraw at once (SQL-O6's allow-list). wait already printed
  each progress line as it came.
- Streaming a running target's whole log, the item's second half, is next
  with `logs` on a running check.
- The guest program's pin moves with it (origin_test's guestPin); the verifier protocol stays 3, since nothing builds or judges a port otherwise.
