# 2026-09-22: fixture programs are written while no fork can start

## What failed

Two `verify/tart` tests each failed once in a full `go test ./...` run
on a loaded 4-core Linux container and passed every isolated rerun:
`TestInterruptedOrChangedImageDoesNotPublishDigest` (noted in the
[Linux host entry](2026-09-22-tests-on-linux.md)) and
`TestRunningMarkerDoesNotHideExitedGuestRunner`. Neither waits on a
timer, a deadline or a poll, so neither is a timing problem that
`testing/synctest` could fix; both do the same thing first: write a
`#!/bin/sh` stand-in for `tart` and run it.

## The cause

Running a file that some process still has open for writing fails with
`ETXTBSY`, "text file busy". `os.WriteFile` opens the fixture
close-on-exec, but a child that another goroutine forks while the file
is open gets a copy of that descriptor and keeps it until it execs. The
package's tests are parallel and start commands constantly, so under
load a parallel test's fork lands in that window, the fixture's
descriptor stays open in the child for a moment after `WriteFile`
closes its own, and the test's first `tart list` fails with
`fork/exec …/tart: text file busy`. The test then sees that error in
place of the cancellation, the "image changed" error or the
`runner-exited` state it asserts. Isolated reruns start no other
commands, so they never hit it.

A load run of the package without any added instrumentation hit it in
`TestInterruptedOrChangedImageDoesNotPublishDigest/changed` and in
`TestImageDigestTracksContentDespiteRestoredModificationTime`, a third
test with the same fixture. A temporary goroutine that forked `/bin/true`
in a loop beside the tests made it frequent: 200 rounds of the five
tests that run a fixture `tart` failed 30 times across four of them,
every failure `text file busy`.

## The change

`writeExecutable` in the package's test helpers writes a fixture
program while holding `syscall.ForkLock` for reading. The runtime holds
that lock for writing across every fork, so no child is created while
the file is open for writing and none can inherit the descriptor. Each
of the package's six fixture programs now goes through it: the image
fixture's `tart` and its running variant, the three `tart` scripts in
`native_test.go`, and the fake `portindex`. The code under test is
unchanged; nothing in it races.

With the helper, the same 200 rounds beside the forking goroutine pass
without a failure. So do six rounds of the package under `-race` at
`-cpu 1,2,4`, run while the whole suite ran six times alongside, and
those six suite runs.

## Left open

Test fixtures in fourteen other packages write a program with
`os.WriteFile` and run it the same way: `cli`, `credential/keychain`,
`git`, `github`, `macos`, `macports/dependency`, `macports/portedit`,
`macports/portindex`, `subprocess`, `tart`, `tart/host`,
`tart/provision`, `workflow` and `workflow/preparation`. Those whose
tests run in parallel with commands are exposed to the same failure.
Sharing the helper across them needs a place for test support that the
repository does not have yet, so they are not changed here.
