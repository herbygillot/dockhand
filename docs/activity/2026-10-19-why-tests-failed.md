# Why a check's tests failed, from its log

The first two lines of the roadmap's v0.5.0 part 1 item 5, "Why a check
failed, from its log", from the exiv2 field notes (2026-10-19). exiv2's
test phase ended at `make: *** [test] Error 8` and a pointer to CTest's
LastTest.log, which went with the VM, so its Portfile was changed to run
`ctest --output-on-failure` just to see why; and most of its 74,000-line
log was archives' installs at debug level.

## A failed test's own output comes back

- The guest program (buildenv/tart/guest.tcl), after a test phase fails,
  appends the last 200 lines of each test log the port's build systems
  left in its work directory, at most three: CTest's
  `Testing/Temporary/LastTest.log`, automake's `test-suite.log`, and
  meson's `meson-logs/testlog.txt`, each under "dockhand: the last lines
  of <path>, the tests' own log:". `work_dir` reads the port's workpath as
  `checkout` reads its worksrcpath.
- `buildlog.FailedTests` reads the failed tests a log names, from ctest's
  "The following tests FAILED:" list and automake's `FAIL:` and `ERROR:`
  lines, each once.
- The runner's `withFailedTests` adds them to a result whose tests failed,
  read from its test step on, as "failed tests, from its log:
  bugfixes-test", under declared tests too, where the target passed and
  nothing read its log.
- Tests: `TestAFailedTestPhaseBringsBackTheTestsOwnLogs` (guest),
  `TestTheFailedTestsAreNamedFromTheLog` (buildlog), and
  `TestAResultWhoseTestsFailedNamesThem` (engine).

## A dependency install is quiet where nothing builds from source

The guest installs a target's dependencies with `-d` only where one has no
binary archive and builds from source, which D10's reading of a
compiler's error and MacPorts' "Command failed:" line need. From archives,
tar's listing and each "activating file:" line no longer fill the log.

The guest pin moved; the protocol stays 3, since nothing is built or
judged otherwise (origin_test.go's guestPin says why).
