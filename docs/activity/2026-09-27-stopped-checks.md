# 2026-09-27: a killed check reads as stopped, not running

Found while [cleaning Tart clones](2026-09-27-tart-clone-cleaning.md): after a foreground check was killed, `status` showed "running (check-18)" and `queue` counted it, though nothing ran it.

## Why

A run records `running` when its driver takes it up, and only its driver settles it. Ctrl-C lets the driver settle it. SIGKILL, a crash, or a closed terminal doesn't, so the record keeps saying running. That is on purpose: the run is resumable, and the next `serve` takes it up where it stopped. The record was right about the run and wrong about the process, and `status` showed the record.

## Changed

- **The engine judges it** (`engine.Stopped`, `engine.JudgeStopped`). A run recorded as running is **stopped** when no live process holds its lease, the same test the runner uses before taking a run up. Judging who is alive takes a session, so it isn't part of `BranchStatus` itself. The commands that show checks ask for it. serve's and diff's reads of status are unchanged.
- **`status`**
  - The checks column says "stopped (check-18)".
  - The attention list says "check-18 stopped: the process running it ended; dockhand cancel check-18 ends it", with `dockhand wait check-18` as the next step.
  - The branch's own view says the same in its Checks and Next lines.
  - The serve line counts it: "queue: 1 run, 1 stopped".
- **`queue`** shows its state as stopped, with the same two ways on.
- **JSON** keeps the recorded state, `running`, and adds `"stopped": true` to the run, in `status`'s `active_checks` and `queue`'s `runs`.
- **The record is left as it is.** Reading status never writes, and the run stays resumable.

## Measured on the Mac

In the scratch clone and database, a check of `tree` on Tahoe was killed with SIGKILL once its clone was up:

```
Needs you
  ! tree-e747  check-19 stopped: the process running it ended; dockhand cancel check-19 ends it  dockhand wait check-19

BRANCH     PORTS  WORK                CHECKS              PR
tree-e747  1      edits, uncommitted  stopped (check-19)  —

serve: not running · queue: 1 run, 1 stopped
```

`dockhand wait check-19` then resumed it as attempt 2. Its sweep removed the dead attempt's clone before it made its own, and `tree` passed. No check clones were left.

## Tests

The dead process is simulated by a session that takes the check's lease, records it running, and ends. The test then checks:

- `status`, in the branch's own view and in the list;
- the serve line's count, and the JSON flag beside the unchanged state;
- `queue`'s row and JSON;
- that `wait` resumes it, after which nothing reads as stopped.

## Seen once, not explained

One full `make test` run failed `TestTidyAsksWhatItCannotKnow` in its cleanup: "unlinkat …/.git/objects: directory not empty". Something was still writing into the test's repository when it was removed. It passed three reruns alone, and the next full run was green. Git's automatic gc starts at about 6,700 loose objects, far above what the test makes, so it isn't the likely writer. It is left as recorded until it recurs.
