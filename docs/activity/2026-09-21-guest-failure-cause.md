# 2026-09-21: a failed build records what MacPorts said

## Why

The guest runner ran each phase with its output redirected into the build
log, so the error it caught was Tcl's "child process exited abnormally". That
string became the step detail, the failure detail, and the text on the
attempt line. A regexp over the log recovered the failing package and phase
from MacPorts' `Error: Failed to <phase> <package>:` line and discarded the
rest of that line. In a run this morning a dependency's distfile was refused
by six mirrors with a 404 or 403 each; the user was told the dependency
"failed to build" and given a log path that retention prunes.

## What changed

- The guest keeps MacPorts' own `Error:` lines for the failing step, from the
  first after the step's marker line up to the last `Failed to <phase>
  <package>:` line, joined as the detail, capped at six. Boilerplate that
  follows (the main.log pointer, the tickets link, "Processing of port")
  is left out. That detail is the step detail, the failure detail, the
  advisory test failure, and the result detail.
- For a distfile named in a `Failed to fetch <distfile>:` line, the guest
  records every `Attempting to fetch <url>` that was followed by MacPorts'
  `DEBUG: Fetching <url> failed: <reason>` line, as `Fetches` on the failure:
  URL and reason in the order tried, capped at twenty. A fallback that later
  succeeded for another distfile has no failed-fetch line and is left out.
  Only the failing step's part of the log is read, after the marker the
  runner wrote before it.
- `record.Failure.Fetches` and `record.FetchAttempt`; the judge clones the
  list into evidence.
- The next-step line names the phase and the cause: "fetch failed: <cause>;
  fix and amend, or abandon" for the target, and "dependency jxrlib failed
  to fetch, not the change itself (<cause>); retry it, or fix that port
  first" for a dependency. A failure without a phase still reads "failed to
  build".
- `status` lists each tried mirror with its reason under the attempt's
  failure line. The summary's attempt line already carried the detail and
  now carries a real one.

## Evidence

- `TestGuestKeepsMacPortsErrorLinesAndMirrorAttempts` runs the guest script
  under port-tclsh against a log shaped like the real one, with an earlier
  step's error line and another distfile's mirror fallback, and checks the
  detail, the package and phase, and the two recorded attempts.
- Phrasebook and status tests cover the wording and the mirror list.
