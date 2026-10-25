# The baseline in every check's path

2026-10-25, Then's item 4 (the reflections' D, with the exiv2 session's
baseline after a rebase, 2026-10-24). `check.baseline = true` ran the
baseline a failed check points to, but only the command's foreground
check read it, so `check -d`, ship's, and serve's checks never ran one.

- **The engine's continuation.** engine `Options.Baseline` (compose, from
  `check.baseline`). The driver, as a check ends failed, calls
  `continueWithBaseline`, which queues the baseline `BaselineCandidates`
  offers (`PlanBaseline`, `EnqueueBaseline`), with the check's origin, so
  a drain, ship, and serve run it next. The foreground check runs the
  queued one itself (`QueuedBaseline`), where it planned its own.
- **The answer is the verdict's.** branchstate's attention row for a
  failure its baseline has alike names `dockhand submit --accept <port>`
  in status's Next, where it named the logs; dockhand accepts nothing
  itself.
- **Across a rebase.** engine `standingBaseline`: an earlier baseline of
  the branch whose failed build of the target, in the environment, at the
  phase, read nothing that differs at the branch's base now, by
  `reuse.Current`, answers the failure (`failsAtBase`) and isn't offered
  again (`BaselineCandidates`); one whose reads changed is offered again,
  saying what: "master's build of exiv2 reads libheif's directory,
  changed since check-490's baseline" (`BaselineOffer.Changed`, said by
  the check's report).
- **Docs:** usage.md's `check.baseline` row.
- **Tests:** TestABackgroundCheckRunsItsBaseline (a `check -d`'s baseline
  run by its drain, and status's Next offering `--accept`) and
  TestABaselineStandsWhereNothingItReadChanged.
