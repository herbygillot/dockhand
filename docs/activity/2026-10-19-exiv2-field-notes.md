# The exiv2 field notes, taken into v0.5.0

The person passed on the notes of a session that updated exiv2 to 0.28.9 with dockhand (a main build at 0a4a05ea, checks on Tart macOS 12 and 27), asking the prioritization thread (2026-10-19, 18:07Z) to "do some thinking on the features. We may also need to recoalesce the roadmap." Each note was checked against main at 5897a828 by reading the code and MacPorts base's source; nothing was run. Docs only.

## The short answer

The eight notes describe one gap. A failed check gives a sound verdict, but it's a poor place to work on the failure:
- the failing test's output stays in a VM that's deleted;
- the log is mostly a dependency install;
- there's no way into the guest;
- each question costs a full check.

The session was an agent, as field testing is, and field testing has met notes 5, 7, and 8 itself.

What to take is about 3 batches, as two items in v0.5.0's part 1:
- **Why a check failed, from its log** (item 4): this can go before v0.4.0's tag.
- **A failed check you can work on** (item 6): this comes after X1.

Two things aren't taken: reuse across a test-only edit, and `--debug-cmd`.

The recoalescing is light. Nothing in layer 1 moves, since none of the notes touch the record or the verdict. Part 1 grows from about 6 batches to about 9, and v0.5.0 to about 25 to 29. Later's `check --keep` entry joins item 6. Linked libraries (part 6) is still the one part to move to Then if v0.5.0 runs long.

## Each note

| | Note | Today, at 5897a828 | Taken |
| --- | --- | --- | --- |
| 1 | A failed test can't be diagnosed | The guest runs `port -d test` into the target's log. The detail is MacPorts' last Error: lines and "Command failed:". Only the target's log and archives come back, then the VM is deleted, so CTest's LastTest.log goes with it. Under `declared`, a test failure is "passed, tests failed", and nothing reads the log. | The guest appends a capped tail of the known test logs (CTest, automake, meson) after a failed test phase. The summary names the failed tests. Item 4. |
| 2 | No way into the guest | `Execute` always stops and deletes the clone. `tart run` is a child of dockhand. | `check --keep` keeps the clone, stopped. `shell check-N [-- cmd]` starts it, runs the command in the work directory, and stops it. Item 6. |
| 3 | A test-only edit rebuilds everything | Reuse compares the port directory's Git tree, so any Portfile byte counts. A target's build and tests are one result. | `shell check-N --test` reruns the test phase alone in the kept guest, with `port -o`. That keeps MacPorts' state although the Portfile changed. It's never evidence. Reuse itself stays as it is (below). |
| 4 | Logs hard to search | The guest installs dependencies with `-d`. Tar's listing and "activating file:" lines are debug-level, and fill the log. The `dependencies` step is already recorded, and `logs check-N <port>` starts after it. | No `-d` on the dependency install when nothing builds from source. `logs --phase` stays in part 2. Item 4. |
| 5 | Logs directory layout | Attempt -2 is X2's infrastructure retry. Attempt -1 was interrupted, and its partial log was never fetched. The reason is in the execution's detail and in `logs check-N`, but not in a port's view or the failure report. | Each port's view says the environment, the attempt, the path, and what ended an earlier attempt. The failure report lists them. An interrupted attempt's log is fetched, or its empty directory goes. Item 4. |
| 6 | `--baseline` hint misleads | `BaselineCandidates` counts any test failure, and checks only that master has the Portfile. | A port whose only failures are tests master doesn't declare (`dockhand.test_run` in the change record) isn't offered a baseline, and the report says why. Item 4. |
| 7 | No long check without serve | `check -d` without serve says "Start dockhand serve" (design v3). `serve --drain` already drives the queue and exits when it's empty. | With no serve, `check -d` starts `serve --drain` in its own session, as automatic cleanup is started. Item 6. |
| 8 | Checking for concurrent instances by hand | Every driving or watching process opens a `sessions` row with its pid, build and heartbeat. The store can't list them, and they record no schema. | `status` lists live sessions with their build and what they hold. A `schema` column says whether a session's build matches the database. Item 6. |
| | `path <branch>` outside the tree (field testing) | The tree is opened before the database, and the store can't list checkouts. | Use the one registered checkout; refuse and name them where there are several. Item 4. |

## Not taken

- **Reuse across a test-only edit.** Telling a test-only edit apart needs a conservative Tcl classifier. The build's result would have to be split from its tests', which means new evidence wording. Worse, MacPorts' tests run in the work directory, which dockhand doesn't keep: it keeps archives, and work trees are gigabytes each. That's several batches, and `shell --test` serves the debugging it's for.
- **`--debug-cmd`.** Its question must be known before the check, so every new question is another full check. The kept guest answers any number of them, at about a minute each.
- **`CTEST_OUTPUT_ON_FAILURE` in the guest's environment.** It would reach the test phase only through `extra_env`, which macports.conf(5) calls unsupported to change. It also covers CTest alone, and reading the test logs covers CTest too. automake's `VERBOSE` is worse: it's a generic name that cmake-1.1 already passes to builds.
