# A library's install names, its dependents, and the files installed

2026-10-27, Then's item 9 (the reflections' E with the revbump
classifier, and the docs dig's I14 and I16).

- **The guest reads them** (guest.tcl `contents`, `libraries`): a passed
  build's `port contents` list, read once for the link scan and now kept
  (`installed`), and each of its own Mach-O libraries' LC_ID_DYLIB install
  name and compatibility version from `otool -l` (`libraries`). Each is
  unknown, never empty, where it can't be read. The host carries them
  (guest follow.go, buildenv.TargetReport, model.TargetResult's
  `Installed` and `Libraries`), kept beside links (schema 45,
  `results.installed` and `results.libraries`, NULL where not read).
- **The earlier reading** is the port's previous result in the same
  environment that dockhand checked: the newest passed build of another
  check, by the reuse query (store `Reusable`). Master's binary archive,
  installed and read before the build where there's no such result, isn't
  done here: a port dockhand never checked there says nothing.
- **evidence compares, purely** (evidence/installed.go `CompareInstalled`):
  a library matched by its stem whose install name or compatibility
  version moved, "libexiv2.27.dylib is libexiv2.28.dylib now", and files
  gone or added, a new soname's file and a versioned directory's taken for
  the same file; no reading either side says nothing.
- **A check's report says it** (engine `InstallChanges`,
  view `WriteInstalled`), under "Installed": a moved library marked for a
  look, with the check compared against and the ports that link it
  directly at the revision's base, "update --revbump-dependents rebuilds";
  files gone or added for information, marked where neither the version
  nor the revision was raised, "raise the revision".
- **The revbump classifier's non-ABI half** (commitrules, warnings):
  `dependencies-unbumped`, depends_lib or depends_run changed with neither
  the version nor the revision raised, and `revbump-build-only`, a
  revision raised for depends_build, depends_fetch, or depends_extract
  alone, each read from the diff, never from the list.
- Tests: TestAPassedBuildKeepsWhatItInstalledAndItsLibrariesNames,
  TestInstalledFilesAndLibrariesAreCompared,
  TestACheckSaysWhatMovedOfWhatItInstalled,
  TestARevisionBumpIsReadFromTheDiff.
