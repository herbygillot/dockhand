# v0.5.0 part 1, item 3: one checked correction (CX-3)

Codex's review of 832f059e, high 3. Dockhand corrects its own prepared
edit in four places, and each checked the correction its own way: the Go
toolchain raise and the stealth corrections compared the whole family
against an explicit allowed change and recorded a report, while dropping a
patch upstream merged checked only the patch list, the version, and the
revision, then replaced `Prepared` itself. A setting that reads the patch
list, or a sibling sharing it, could change unsaid.

- **portedit/correction.go:** a `correction` is the Portfile as corrected,
  the files it deletes, the change it's allowed (`allowed`, the port as
  prepared with that change made), and its words. `Service.correct`
  evaluates it, compares every port, every option, and each revision
  against the allowed change (`correctionReport`), and only where nothing
  else moved publishes the files, the snapshot, and the report together;
  `Result.Prepared` is set nowhere else but `Result.report`.
- **Patch removal** is on it first: allowed the patch list less those
  dropped, however Tcl spaces it, and the option gone where none is left.
  Where more moves, the patches stay and it says so: "Kept fix.diff, which
  the new source already holds: dropping it also changes …".
  TestAPatchWhoseRemovalChangesMoreStays, with `use_configure` set by
  the patch list's length.
- **The toolchain raise** and **both stealth corrections** (the checksum
  refresh's revision and dist_subdir, and a version update's dist_subdir
  dropped) are on it too; `distSubdirReport` became `distSubdirCorrection`.
  Their tests pass unchanged.
- **CX-9's narrow part:** a go.mod the raise can't read, as one with a
  repeated go statement or a forge's error, failed the update in
  depblock's words, though the raise never refuses the bump. It's now said
  as a warning, and `GoToolchainUnchecked` with the problem, which the
  assessment holds as "go.mod couldn't be read (…), so go.toolchain_min
  wasn't checked against it". TestAGoModItCantReadLeavesTheMinimumUnchecked,
  and TestTheGoMinimumIsJudgedAsItStands's unchecked case.
