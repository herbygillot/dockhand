# The command package review, placed

The command package review (2026-10-15, at 5eb101a), checked in as
[docs/reviews/2026-10-15-command-package-review.md](../reviews/2026-10-15-command-package-review.md),
and its coalescing against the code and the roadmap (the project's
plan/command-package-coalesced-2026-10-15.md), at the person's "Send it"
(07:13Z), placed in the roadmap:

- v0.4.0 part 1 gains one batch, second after checksums: how a command asks
  and exits (one `decide`, saying how it was answered; exit constants). C2a
  takes "a hold is said once" and `engine.ComparisonLines`. Submit, status and
  outdated, branches (`engine.Selector`), and hygiene each take the review's
  items that open the same code. No new package-level test seams from here.
- Part 2: `CheckStanding(profile)` with build profiles; `CaptureAuto`,
  `Enqueue(Replace)`, `Plan.BuildCount`, one drive-or-follow with ship's
  engine; the ≈105 judgment lines in deleted code deleted; the selector rollout
  as wiring; `engine.Queue()` as SQL-O3 with SQL-O4.
- Part 3: X1 with the host value; `clean --delete` reads `CleanStep.Kind`.
- v0.5.0: `RunLogs.ForPort` in 5.1; attention, `Next:`, `TidyPlan`,
  `attention.Kind`, and `Refusal` in 5.5, with `BaselineComparison` beside it.
- Not taken: a package split, a workflow type, a command registry, a shared
  branch shape. v0.4.0 is about 17 batches.

The review is checked in as the library's copy, unedited.
