# A setup.py that changes only its version holds nothing (v0.4.0 part 1, c)

Field testing at f41fbad6: py-kubernetes 37.0.0 to 37.0.1 was held, "!
setup.py changed; the build may need the Portfile to follow", where
setup.py's whole change was `CLIENT_VERSION = "37.0.0"` to `"37.0.1"`, a
constant its `setup(version=…)` reads.

- `project.WritesVersion` is the rule a changed line of a build file is
  read by: it differs only by the old version written as the new, and
  declares the project's version as its build system does
  (`DeclaresVersion`), or is in a file that may write the version on any
  line, setup.py and pyproject.toml (`versionWrittenAnywhere`).
  pyproject.toml is compared as a manifest, so setup.py is the one this
  reaches today.
- `sourcecompare.versionOnly` reads each changed line by it, with
  `DeclaresVersionPart` for CMake's version parts as before, so the change
  is said, "· upstream's setup.py changed only the version it names:
  "CLIENT_VERSION = \"37.0.1\""", and holds nothing. A line changed in
  anything else still holds.
- The item's second half, a hold said once rather than once a subport,
  came with typed holds (b, 97afa9ac).

Test: TestASetupPyChangingOnlyItsVersionHoldsNothing.
