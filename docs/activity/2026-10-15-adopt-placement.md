# adopt places a worktree without a false failure

Adopting the dockhand port's branch (dockhand-util, macports-ports #34756)
printed "It is not checked out anywhere: conflict: branch …'s creation time
is fixed", though it had made the worktree: adopting again found it checked
out there. `Adopt` built the new branch's record with the clock's time, finer
than the store's milliseconds, and `placeWorktree` wrote that in-hand record
back, which the store refused as changing the creation time. The tests' clock
is whole seconds, so none saw it.

`placeWorktree` now reads the stored record in its transaction and sets the
worktree on that. TestAdoptTracksABranchAsItStands adopts with a clock finer
than a millisecond, and fails without the fix.
