# A maintainer's instruction in a Portfile's comments is quoted

2026-10-27, Then's item 11, the docs dig's I7 (the person's "notify the
user if it seems that we can recognize an instruction in the comments").

- **The scan** (portfile `Instructions`): the comments at a Portfile's top,
  and in the updated subport's own block, a comment of several lines one,
  whose words carry a short phrase ("do not update", "don't upgrade",
  "increase the revision of", "keep in sync with", …), as osxfuse's "do
  not update past 3.8.3" and dav1d's "increase the revision of libheif,
  ffmpeg…" read. Pure, of the text alone; it reads no intent or version
  ceiling, and holds nothing.
- **Where it's said**, quoted with its place,
  "sysutils/osxfuse/Portfile:2: Do not update past 3.8.3.": update's notes,
  which ship and bump print as update does ("! The Portfile's comment
  says, at …"), serve's report beside the branch it prepared, and status's
  detail under "Comments" (the status report's `Instructions`, the
  top-level comments of each Portfile the branch changes).
- Not done: the entry's test over a tree snapshot pinning false positives;
  the phrase list's are measured by the portfile test's cases alone.
- Tests: TestAMaintainersInstructionInACommentIsFound,
  TestAPortfilesInstructionIsQuotedWithItsPlace.
