# 2026-09-22: a test support package, and every fixture program written through it

## Why a package

The [text file busy entry](2026-09-22-text-file-busy.md) fixed
`verify/tart`'s fixture programs with a helper local to that package and
left the other packages writing their stand-in programs the same unsafe
way. The helper is one function, but copying it into sixteen packages
would scatter the one place that explains the race. Dockhand
now has `internal/testsupport` for helpers that several packages' tests
share. Only tests import it, and its dependencies are the standard
library and testify.

## The change

`testsupport.WriteExecutable(t, path, program)` replaces `verify/tart`'s
`writeExecutable`. It writes the file with mode 0700 while holding
`syscall.ForkLock` for reading, so no child is forked while the file is
open for writing and none can inherit the descriptor.

The package's test starts four goroutines that start `/bin/sh` in a loop
and meanwhile writes and runs a hundred programs through the helper. With
the lock removed, it failed with "text file busy" in twenty runs out of
twenty on an idle machine; with the lock, it passed twenty runs, and
five more under `-race`.

Every test that wrote a program with `os.WriteFile` and an executable
mode now calls the helper: 36 call sites in `cli`,
`credential/keychain`, `git`, `github`, `macos`,
`macports/dependency`, `macports/dependents`, `macports/portedit`,
`macports/portindex`, `subprocess`, `tart`, `tart/host`,
`tart/provision`, `workflow` and `workflow/preparation`, plus the six in
`verify/tart`. `macports/dependents` was not among the earlier entry's fourteen:
its search missed calls that put the mode after a multi-line script. The Git
hook in `workflow`'s contribution lifecycle test was written 0755 and
is now 0700, which Git runs the same way. Files that are never run keep
`os.WriteFile`: Tcl scripts passed to an interpreter, blobs whose mode
only Git records, and the file in `git`'s worktree test whose mode the
test changes only for Git to record.

`docs/development.md` states the rule, and `docs/components.md` lists
the package and its dependencies.

## Evidence

A temporary, uncommitted file in each changed package started four
goroutines forking `/bin/true` in a loop beside the tests. Thirty rounds
of the twelve quick packages, one package at a time:

- before, five packages failed: `credential/keychain`, `github`,
  `subprocess`, `tart` and `tart/host`. Seven tests failed fourteen
  times, and the output had 28 "text file busy" lines;
- after, all twelve passed without one.

With the change, all sixteen changed packages also passed three rounds
together under the same storm, and `cli` and `git` passed ten rounds
each. The whole suite passes.
