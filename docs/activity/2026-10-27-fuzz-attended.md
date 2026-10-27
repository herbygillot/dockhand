# A fuzzed patch is re-emitted for an attended update

2026-10-27, roadmap's follow-up to Then's item 8 (prioritization,
d85b003c).

- The update's own `Unattended` reaches portedit through editprep as
  `Request.Attended`, and patchcheck as `Request.Fuzz` (`Port`'s new
  argument): an attended update re-emits a patch that applies only with
  fuzz as it re-emits one at an offset, each fuzzed hunk where patch put
  it, its prediction the tree the old patch made with fuzz, checked byte
  for byte. The refresh says each fuzzed hunk, "hunk 1 of dir/c.txt with
  fuzz 1 written where patch put it: look it over in the diff"
  (`Refresh.Fuzzed`).
- An unattended update holds it unwritten, as before (patch-fuzz); a
  revision's assessment reads patches without fuzz refreshes.
- Tests: patchcheck's TestAPatchIsRefreshedToApplyAsWritten and portedit's
  TestPreparationChecksDeclaredPatchesAgainstTheNewSource, attended and
  not.
