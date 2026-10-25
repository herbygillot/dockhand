# A baseline whose branch's worktree is gone: the look

2026-10-25, roadmap 5e6287d5, from B11 on the M1 with rc1: a `check -d`
drain queued B11's baseline after the stage's reset had removed the
branch's worktree, and the baseline failed fast and was tried again,
three attempts.

Not reproduced in the engine. TestABaselineNeedsNothingOfTheBranchsWorktree
removes a separate worktree's directory before the failed check ends: the
check's continuation plans and queues the baseline, which runs in one
attempt, and nothing checks the worktree out again. So neither the
baseline's planning nor its run reads the worktree, with the script
provider. What failed on the M1 is elsewhere: the Tart provider's
attempt, or more of the branch than its worktree gone with the stage's
reset (its Git branch or its records), each inferred. B11's log on the M1,
its baseline's attempts and their details, says which.
