# 2026-10-02: package names and layout review

Mapped the 73 internal Go packages at `034417d0d4378376c6529512491ee89b5fea57f1` from an isolated source export while development continued in the checkout. Added the [package naming and layout proposal](../reviews/2026-10-02-package-names-and-layout.md).

The proposal prioritizes names that expose their subject (`buildinfo`, `textedit`, `depblock`, `portprep`, `updatescan`, and `guestssh`), promotes the shared distfile downloader out of `portedit`, and supports the already-planned `portcreate` rename. It distinguishes these from optional changes, maps the remaining package responsibilities, and identifies linker-symbol and documentation updates required during migration.

Documentation only: no package moves, application changes, tests, configuration/schema changes, or commits were made. Validation used source/API/import inspection and documentation checks; no runtime tests were needed. Existing development changes were left alone, and no Git worktree was created.
