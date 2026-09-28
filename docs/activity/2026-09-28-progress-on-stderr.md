# 2026-09-28: progress on standard error

The code-organization review's finding 31: nothing in dockhand showed its progress reports.
- `progress.Report` and its kin dropped every report unless a reporter was on the context, and only the survey tool installed one.
- So the 48 reports under `internal/` reached nobody, among them "Building the PortIndex; this may take several minutes". A person couldn't tell a hung command from a slow one.
- v2 had printed them, with `-v` and `-vv`, and its sink went with it (`86813c9c`).
- Design §12 says progress goes to standard error.

**What changed.**
- **`command.Run` installs a reporter** on each command's context. It prints info reports to standard error, adds verbose ones with `-v` and debug ones with `-vv`. Results stay alone on standard output, and `--json`'s envelope is untouched.
- **A check a command drives itself** reports under `progress.Quiet`: its journal and report tell the story, and the engine's and providers' info reports are the work behind the scenes there, shown with `-v`. Serve, which drives for everyone, keeps every line.
- **One owner for standard error's redrawn line.** `outdated` redraws its count in place ("Looking up each port's newest release: 12 of 1,004"), and a report arriving meanwhile would have landed on the same line. The line is a `statusLine` now: the count draws through it, and a report clears it, prints, and draws it again.
- **An interrupted look prints what it found.** `outdated` and `update --outdated` dropped the ports a look had finished, though the engine returns them with the interrupt. They now print them, saying how far it got ("Interrupted after looking up 312 of 1,004 ports"), and `update --outdated` starts nothing. The count is cleared too, where it used to be left for the error to land on.

**Tests.**
- `TestProgressGoesToStandardError`, with a preparer that reports as MacPorts' editor does: the info report without `-v`, and the verbose one with it.
- `TestAnInterruptedLookPrintsWhatItFound`, for both commands.
- `TestAReportPrintsAroundTheCount`.
