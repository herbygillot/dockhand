# Master's archive, read on the host, as the earlier reading

2026-10-27, Then's item 9's second follow-up (prioritization, 34690651,
its read on the host).

- **binaryarchive reads an archive** (read.go): `ArchiveName`, as
  MacPorts' buildbots name one, `jq-1.8.1_0+docs.darwin_25.arm64.tbz2`, its
  variants signed in name order; `URL`, packages.macports.org's
  `<port>/<name>`; and `Contents`, its files as the destroot places them,
  MacPorts' +CONTENTS and the rest of its metadata left out, and its
  libraries' install names and compatibility versions from their
  LC_ID_DYLIB, through archive/tar, compress/bzip2, and debug/macho, which
  keeps that command as raw load bytes, read here.
- **The engine reads it** (installed.go `masterArchive`) where dockhand
  never checked the port in that environment: master's version, revision,
  and default variants as the evaluator reads them there, the archive
  fetched once and kept by its name under the archive directory's
  `master/`, one not served marked `.none` and not asked for again, its
  /opt/local paths rewritten to a `local` prefix's. The finding is
  "against master's archive jq-1.7.1_0.darwin_25.arm64.tbz2". The
  configure summary has no earlier reading from it, an archive holding no
  build log. `Options.Packages` points it at another site, for tests.
- **status fetches nothing** (`InstallChanges`' new fetch argument): it
  reads only an archive kept; the check's report and submit fetch.
- Tests: TestAnArchiveIsNamedAsMacPortsNamesIt,
  TestAnArchivesFilesAndLibrariesAreRead (a real dylib, tbz2 from the
  system's bzip2, the test's alone), and
  TestAFirstCheckComparesAgainstMastersArchive.
