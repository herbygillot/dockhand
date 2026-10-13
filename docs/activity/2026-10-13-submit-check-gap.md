# submit --check runs the check an earlier one left unfinished

Field testing (grafana 13.2.4, macports-ports #35488, on an rc8 build): `submit
--check --on tart:12 --on tart:26 --yes`, with no terminal, printed its
preview, then "✗ grafana is changed, and no check of these files built it
everywhere it's required … nothing was checked or submitted", and ran no
check. The terminal wasn't the cause: check-323 had built the committed files
on macOS 12 alone, so `evidence()` found evidence and applied the publication
rule, and `PendingCheck` set `CheckNeeded` only where no evidence existed.

- `evidence.Evidence.Unfinished`: some target asks for a check, and none that
  didn't pass has a failed result. Another check would settle it.
- `engine.evidence`: under `PendingCheck`, unfinished evidence sets
  `CheckNeeded` rather than blocking. The check that follows reuses what
  stands. A failure of the same files still stands and is refused, as
  TestSubmitCheckReportsOneShape has it.
- The Checks line (`checkWords`) said "check-323: grafana did not pass on tart
  macOS 12 …, tart macOS 26 …" though 12 passed. It now says each port's
  failed environments apart from those it isn't built on for these files.

Tests: TestEachEnvironmentTakesItsNewestCheck (a `--check` plan over a
required environment no check built), TestCheckWordsSayWhatWasntBuilt.

## CI on b1ac0ce2: a heartbeat test's fixed wait

CI's Intel job failed TestKeepAliveBeats (internal/coord), unrelated to the
change: the test gave KeepAlive 200ms, and a slow runner's store writes ran
past it, so no beat was recorded. It now waits for a beat, up to ten seconds.
