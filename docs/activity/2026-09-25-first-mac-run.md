# 2026-09-25: v3 on the Mac, with the guest channel under it

The first local Mac session after Design v3.

## Step 2's commits on top of v3

The guest channel (roadmap step 3 under v3) had been built and proven on
this Mac on 2026-09-24, on v2's provider, in nine commits never pushed,
while the cloud session built v3 on `main`. They were rebased onto it.
The code did not overlap: the channel, the own Tart home, and setup over
SSH are in `tart`, `tart/channel`, and `tart/provision`, which v3 keeps,
and the SSH verification is in v2's `verify/tart`, which v3 keeps as the
reference for its Tart provider. The conflicts were the roadmap, `go.sum`
(both sides had dropped a module), and the vendor directory, which was
regenerated with `make vendor`.

The roadmap's step 3 moved the channel after the freeze so as not to build
it twice. It now says what is built and proven, and that what remains is
the v3 Tart provider on top of it, in step 5's part 5.

## The suite on macOS

`go test ./...` on the Mac had one failure,
`TestBareDockhandSaysWhereTheWorkingToolIs`: a bare `dockhand` printed
the status of the person's real ports checkout instead of the rebuild
notice. The command tests ran with the person's `HOME` and their
`MACPORTS_TREE`, so the test registered that checkout in a new
`~/.dockhand/dockhand.db` in the real home. Linux CI has neither.

`internal/command` now has a `TestMain` that gives the tests their own
`HOME` and clears `MACPORTS_TREE`, `DOCKHAND_DB`, `DOCKHAND_CONFIG`,
`DOCKHAND_UPSTREAM`, `DOCKHAND_GITHUB_CLIENT_ID`, `GH_TOKEN`, and
`GITHUB_TOKEN`. The suite passes on macOS, and a run leaves nothing in
`~/.dockhand`. The stray database, which held only the test's observer
session, was moved aside to `~/.dockhand/dockhand.db.from-test-2026-09-25`
for the person to delete; nothing was written into the checkout.
