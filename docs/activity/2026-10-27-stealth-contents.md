# A stealth update says whether the archive's contents changed

2026-10-27, the docs dig's I19 (roadmap 933d4f0a). `checksums` took an
archive whose checksum changed under the same name as a stealth update and
revbumped, deciding without evidence, and said only the two checksums, so
a regenerated archive, gitea-tea's renamed root and 24 bytes of tar
metadata, read as a re-roll, which could be a security event.

- **The comparison** (portedit's `stealthContents`): each changed archive
  is fetched again, kept, and the one MacPorts shipped is fetched as
  shipped (distfetch's `Shipped`, from MacPorts' mirror under the port's
  dist_subdir, since upstream serves the new one). Their trees are read by
  `archive.Tree`, each regular file's SHA-256 by its path below the one
  top directory, so a renamed root, compression, and member times and
  owners don't count, and compared (`archive.Differences`).
- **The same files**: no revbump; dist_subdir is set, numbered, so mirrors
  keep both; checksums says "Its contents are unchanged; only the
  archive's packaging differs, so the revision was not bumped."
- **A changed file**: named, a few, "Its contents changed: src/main.c.",
  revbumped as before, with `dockhand diff --source`.
- **The shipped archive gone, or unreadable**: revbumped as before, saying
  "! Could not compare it with the archive MacPorts shipped: …".
- `--json`'s stealth carries `unchanged`, `differ`, and `compare_problem`.
- Where the person keeps the revision (`--no-revbump`, or one bumped by
  hand), nothing is compared, as nothing is decided.

TestAStealthUpdateSaysWhetherTheContentsChanged, with real archives and a
mirror stand-in.
