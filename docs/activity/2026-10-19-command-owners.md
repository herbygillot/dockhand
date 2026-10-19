# ship's submission in ship.go, newRoot, a branch's port, one refresh reader

Four more lines of the roadmap's v0.5.0 part 1 item 4, "One owner for
each rule", from the architecture review of 5897a82.

## R3-12: ship's submission in ship.go

`pendingSubmit`, `prepareSubmit`, and `submitPassed`, which only ship
calls, were in submit.go, and submit.go called ship.go's `queueHead` from
them. They moved to ship.go. ship.go still reads submit.go's preview and
words (`writeSubmitPlan`, `askTested`, `applySubmit`, `upstreamLines`,
`checkWords`, `pullRequestWords`); submit.go reads nothing of ship.go's,
so the two no longer read each other. A move, so no test of its own.

## R3-22: `newRoot`

`var inspectRoot func(*cobra.Command)`, set by the help-reference test and
read inside `Run`, was the one package-level test seam added since
959026d.

- `newRoot(settings, streams)` builds the command tree, with
  `runStreams(streams, args)` preparing the streams it reads; `Run` calls
  both, and reads `-v` back from the root's flags.
- The help-reference test's `rootForReference` calls them itself, adding
  the `--help` and `--version` flags cobra adds as it executes.
- Grep for the old shape: `inspectRoot` finds none.

## R3-37: a branch's one port, without a whole status

`branchPort` ran `BranchStatus`, checks and evidence among it, to read
`OnlyPort`.

- `Engine.readFiles` is the part of `BranchStatus` that reads a branch's
  files, scope, and change records; `BranchStatus` calls it, and
  `Engine.BranchPort` calls it alone.
- Call sites switched: command's `branchPort`.
- Test: `TestABranchsOnePortIsReadFromItsFiles`.

## R3-39's second half: one reader of `RefreshPullRequests`

status's `refreshPullRequests` (for `--refresh` and clean) and
`readStalePullRequests` (a plain status's own read) each called
`RefreshPullRequests` and said its changes.

- `readPullRequests` makes the read and says the changes; the two say
  what couldn't be read, each in its own way, as before. With `--refresh`,
  each branch's "could not read" line now follows every change, where the
  two were interleaved branch by branch.
- The command's `attentionFor`, R3-39's first half, is part 4.1's.
