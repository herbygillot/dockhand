# A Description without attribution trailers, and a plain note

Prioritization's Next fix (4aca5610), from field testing.

- `commitBody` (macports/prdescription) leaves a commit's attribution
  trailers out of the Description: Assisted-by, Co-authored-by, and
  Signed-off-by, in any case, beside Generated-By
  (`commitmsg.IsAttributionTrailer`). A reference trailer, such as
  `Closes:` with a Trac URL, stays.
- Where nothing else would be said, the `--note` is the Description
  itself, unquoted. That's the case with no new port and one commit whose
  body is empty once its trailers are out. Its lines that would begin a
  heading have their leading #s written as `&#35;`, which renders the
  same. So `sectionSpan` reads the note to the real next heading, and
  `ownedSpan` finds dockhand's Tested on, not a note's line. A re-submit
  then reads the Description as dockhand's own (`Current`). Beside other
  text, the note keeps its "Author's note:" quote.
- docs/usage.md says so. The note tests in engine and command now expect
  the plain note, since committedUpdate's commit carries Generated-By
  alone.
