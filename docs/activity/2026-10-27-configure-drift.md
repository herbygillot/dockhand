# The configure summary's drift

2026-10-27, Then's item 10 (the reflections' C).

- **The guest keeps it** (guest.tcl `summary`): a passed build's log's
  configure summary lines, each once and in order: autoconf's `checking
  for|whether|if … ...`, CMake's `-- Found X` and `-- Could NOT find X`,
  and meson's `Dependency X found: YES|NO`, each said as what was found,
  not where or at what version, since a CMake path or a meson version
  moves with every build. Kept as `results.configure` (schema 46), NULL
  where not read; the guest's protocol stays 3.
- **evidence diffs it** (`CompareConfigure`) against the same earlier
  build the install names read (Then's item 9's: the port's newest passed
  build of another check in that environment, a baseline's among them),
  lines found and lines lost; an answer that moved is one of each, and no
  earlier summary says nothing.
- **The check's report says it**, in the section now headed "Against the
  earlier build" (view.WriteInstalled), marked for a look: "duckdb:
  configure no longer says "-- Found ICU"; now says … on macOS 26 arm64
  against check-12".
- Tests: the guest test's summary (the fake port's new CONFIGURESAYS),
  TestTheConfigureSummaryIsDiffed, and the engine's InstallChanges test.
