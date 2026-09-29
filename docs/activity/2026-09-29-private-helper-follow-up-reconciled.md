# 2026-09-29: the private-helper follow-up, checked and reconciled

Codex's [follow-up](../reviews/2026-09-28-private-helper-follow-up.md) to its [private-helper review](../reviews/2026-09-28-private-helper-ownership.md) read `259ee3ba`, and was checked here at `11fb35f9`. Nothing it cites had changed in between: `sourcecompare`, planning's `plan.go`, `commitrules`, the Tart provider, and `update.go` are as it read them.

## What holds

- **Its table.** Five findings are addressed. Four are open where the roadmap schedules them, and each is still in the code where it says:
  - eligibility reading `known_fail` and `supported_archs` itself, in `engine/plan.go` (finding 1);
  - revision-only changes found by matching lines, and `commitrules`' version regexes (finding 2);
  - archive keys made in the SSH channel (`tart/channel/keys.go`), and the site signed in the Tart provider (finding 6);
  - three SSH waits, in provisioning, the build provider, and the facts tool, of which only provisioning's stops at a refused login (finding 8).
- **The gap left in finding 3.** A probe in a scratch worktree ran its two cases through `manifestChanges`: a requirements.txt that read base.txt, and a pyproject.toml whose dependencies were dynamic, each only in the old version. Each compared as nothing, so nothing held. The same gaps in the new version hold. D4 already says what couldn't be checked holds, so nothing is left to decide, and the fix is the review's: the old version's gaps kept too, in `sourcecompare`'s own `reading`.
- **What should remain next.** Its criteria for the scheduled items sharpen what item 6 says:
  - for eligibility: Tcl's booleans, `on` included, `supported_archs` as a list, and an option that couldn't be read as unknown;
  - for Portfile inspection: a revision line inside Tcl data isn't a command, and a version is read only where it's literal, so moving today's regexes isn't enough.

  What it says of archive signing and SSH readiness, their items already say.
- **Sizes.** `engine` is 11,140 production lines now, against its 10,900, after the week's fixes; `command` is 8,142 and `portedit` 3,329, as it says. It recommends no new package beyond the planned `macports/binaryarchive`, and the roadmap has none.

## Taken

- The old version's gaps, next, as the comparison came first before.
- The criteria, into item 6's entries for findings 1 and 2.
