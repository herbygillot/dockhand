# 2026-09-27: the smaller rules the design promises

This covers the rest of the roadmap's "rules the design promises and the code doesn't keep", after one check per branch, which has its own note. The numbers are the code-organization review's findings. Each is its own commit.

**An exit code survives wrapping (finding 17).**
- `ExitCode` found an `ExitError` only by a bare type assertion, so one wrapped with more words would have exited 1.
- Exits 2 (a failed check), 3 (attention, such as a held `bump`), and 130 (an interrupt) need to survive wrapping, since scripts read them.
- No path wraps one today, but the next `%w` would have broken it silently. It now uses `errors.As`, and `TestAnExitCodeSurvivesWrapping` covers it.
