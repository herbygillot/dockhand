# A rebuild whose every dependent was left out is listed too

2026-10-27, roadmap's entry of that name (prioritization, 7c7e5506), from
the note on 1f90bc79. `edits.excepted` sat on each revision bump's edit
alone, so `update --revbump-dependents --except` naming every dependent
left nothing to keep them on, and the description said nothing of it.

- RevbumpLinked, where nothing is bumped and something was left out,
  records them on the port's latest version update edit in the branch
  (`keepExcepted`, through the store's new `SetExcepted`), so no schema
  change.
- `leftOut` reads an update edit's as the rebuild of its version, "Left
  out of the rebuild for jq 1.8.1 with `--except`: **gojq** and **yq**.",
  and a rebuild with a bump edit is still listed once.
- TestRevbumpLinkedBumpsEachLinkedPortForTidy covers both.
