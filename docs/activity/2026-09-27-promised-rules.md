# 2026-09-27: the smaller rules the design promises

This covers the rest of the roadmap's "rules the design promises and the code doesn't keep", after one check per branch, which has its own note. The numbers are the code-organization review's findings. Each is its own commit.

**An exit code survives wrapping (finding 17).**
- `ExitCode` found an `ExitError` only by a bare type assertion, so one wrapped with more words would have exited 1.
- Exits 2 (a failed check), 3 (attention, such as a held `bump`), and 130 (an interrupt) need to survive wrapping, since scripts read them.
- No path wraps one today, but the next `%w` would have broken it silently. It now uses `errors.As`, and `TestAnExitCodeSurvivesWrapping` covers it.

**A capture with `--include` checks that nothing moved (finding 43).**
- Capture reads the working files twice, and refuses when they changed between the reads. Design §7 promises this.
- The check was off exactly when `--include` added untracked files: the augmented tree could never equal a plain second read, so the comparison was skipped. The second read still ran, for nothing.
- Now the second read repeats the whole capture, included files too, and compares the two trees. That also catches an included file changing after it was read.
- `TestACaptureOfFilesThatMovedIsRefused` changes files between the reads through a test-only hook: a tracked file, an included one, and a tracked one beside an included one. No test had covered the check at all; with the old check, the included case isn't refused.

**`adopt` and `rebase` report what they did (finding 33).** `create`'s result when interrupted was fixed with the one-shape rule ([note](2026-09-27-one-json-shape.md)).
- **`adopt`.**
  - Adopting a branch already tracked returned before counting, so `adopt --json` said it had 0 commits and no ports.
  - A branch renamed with Git was counted, but its ports weren't.
  - `adopt --pr` of a pull request already tracked did the same.
  - Each path now counts the branch's commits above its base, and the ports they touch, through one `changes`.
- **`rebase`.**
  - It counted the branch's commits before replaying them, and the replay drops a change master already has. So "Rebased jq-update (2 commits)" could name one it didn't keep. It now counts the commits it replayed.
  - A branch already on master recorded a rebase from master onto itself on every `rebase`. `history`'s `SetBase` now changes nothing, and records nothing, for a base the branch already has.
- **Tests.** `TestAdoptTracksABranchAsItStands` checks the tracked branch's count, and `TestARebaseCountsWhatItReplays` checks both rebase fixes. Each fails without its fix.

