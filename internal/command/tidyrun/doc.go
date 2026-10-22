// Package tidyrun is tidy's stage, which tidy, ship, and submit each run:
// a branch's edits committed as tidy plans them, previewed as tidy
// previews them, applied unasked where the plan allows (AppliesUnasked),
// asked on a terminal otherwise, and made through history.Make. The tidy
// verb is its front, as check is checkrun's (architecture's answer,
// 2026-10-22). It imports no verb.
package tidyrun
