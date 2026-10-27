# An amend's missing passes by platform, and two wordings

2026-10-27, the next two of field testing's outdated-ports block
(roadmap 08f5cdfd).

- **After an amend, what still needs a pass names its platforms.** status
  and submit said "check-12 passed; the files have changed since", with
  Next `dockhand check`, so a check on build.default's platforms was
  suggested where the pass was on two releases. branchstate's
  `FilesChangedSince` row now names where the latest check built ("passed
  on macOS 14 and macOS 26; the files have changed since, with no check
  there"), and its Next carries their `--on`, as BuildAgain's does
  (`CheckOn`). Where the latest check's evidence isn't read, as an archived
  branch's, it reads as before.
- **Two wordings.** A license file whose licenses stand, with lines added
  and none removed, says "with 2 lines added, none removed, the first
  "…"", where it said "they cover less of it, 73% of its words" or
  "changed beside it" (sourcecompare's `linesAdded`). A patch whose target
  the new source lacks names it, "the source has no
  bridge_python_generic_hash_mp.c, which it patches", where patch's own
  "No file to patch" named none (patchcheck's `absent`).

TestAnAmendNamesWhereItsPassWas, TestALicenseWithLinesAddedSaysThem, and
patchcheck's TestCheckReportsEachPatch.
