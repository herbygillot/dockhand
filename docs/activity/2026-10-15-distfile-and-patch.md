# A distfile MacPorts doesn't extract, and patches where MacPorts applies them (v0.4.0 part 1, c)

The Submit group's last two items, one change to how a port's distfiles
are read and patched, as MacPorts' evaluation says.

- **A distfile MacPorts doesn't extract is read as the file it is**
  (field testing at 285a496f: bashunit 0.51.0 to 0.52.0 fetches one shell
  script, extract.only empty, and was held, "Upstream archives not
  compared: … archive/tar: invalid tar header"). `macports.PortInfo.Extracts`
  says whether extract.only names a distfile; `project.ReadFile` reads one
  that isn't extracted as a flat reading that keeps it where an archive's
  reading would, a license or build file, and nothing otherwise.
  `engine.readDistfile` is the one reader update's `readPairs` and the
  assessment's `readPlans` share, so the script is compared as one file,
  holding nothing for being no archive. Sniffing for a binary stays for
  what is extracted. A single file with no license isn't given the forge's
  tag archive to compare licenses by: that's a choice to make, not
  assumed.
- **Patches aren't checked against what MacPorts doesn't extract**:
  portedit's `checkPatches`, the assessment's `patchesFor`, and the
  fetched archives `readPlans` hands on leave a distfile out unless
  `Extracts` names it.
- **herdr: patch.args.** MacPorts' patch command line is patch.pre_args,
  then patch.args, and herdr sets `patch.args -p1`; dockhand read
  patch.pre_args alone, -p0, and said patch-macports-update.diff "No file
  to patch… 7 out of 7 hunks ignored" (field testing at 5eb101a3).
  `patch.args` is now read (`macports.ReadOptions`), and `patchcheck.Port`
  gives it after patch.pre_args, the last -p deciding, as patch's does.
- **maddy: a GOPATH worksrcdir with extract.rename.** Evaluated with
  MacPorts, maddy is `worksrcdir gopath/src/github.com/foxcpp/maddy` with
  `extract.rename 1`: the golang PortGroup renames the archive's top
  directory to the whole worksrcdir. patchcheck took the worksrcdir's
  second part on as a subdirectory below the top, so build.sh at the top
  wasn't extracted and the patch found no file (field testing at a9cef1f8).
  The subdirectory is now `macports.SourceSubdirectory`'s, none for a
  GOPATH layout, as the project reading already took it. lima's case,
  without extract.rename, was batch 80's.

Tests: TestADistfileMacPortsDoesntExtractIsComparedAsAFile,
TestADistfileIsExtractedWhereExtractOnlyNamesIt,
TestAFileThatIsntAnArchiveIsReadAsItself,
TestARenamedGOPATHSourceIsTheArchivesTopDirectory,
TestPatchArgsStripLevelIsMacPorts.
