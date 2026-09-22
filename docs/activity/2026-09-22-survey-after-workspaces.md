# 2026-09-22: the survey after workspaces, run roots, and the index fix

## Why

Since the [baseline](2026-09-21-baseline-survey.md), every evaluation
runs somewhere else: candidates in overlays that hold only the port and
`_resources`, workspaces shared through a registry, scratch under a run
root, and an index that describes the platform as base does. Each was
tested on fixtures and a few real ports. The survey over the whole tree
is the only measurement that catches a silent regression at scale, so it
was run again on the same commit, `842164fd61d`, checked out in a
separate worktree so the user's checkout stayed untouched.

## What the first three minutes found

The comparison script was run against the partial journal as soon as
a thousand ports were in it, and two ports had gone from input-found to
unsupported: Go ports with a local patch, refused with "patches must be
inside the frozen port directory". That was the `filespath` item the
morning's open list had carried and I had closed as not a defect an hour
earlier. The derived baseline that the dependency path evaluates in an
overlay was handed the workspace's port directory at two sites in
`dependencies.go`; the overlay's `filespath` compared as outside it. The
run was stopped, both sites now use the projection the baseline was
evaluated in, the refusal names both paths, the Go dependency test has a
scenario with a real patch under `files/` that fails without the fix,
and the survey was started again. The [offline guard
note](2026-09-22-offline-assessment-guard.md) has the correction.

## The run

41,733 ports, the index built in full first because the new cache
identity had no generation and the mirror seed cannot bracket a commit
older than the mirror's index.

| | 2026-09-21 baseline | 2026-09-22 |
|---|---|---|
| input-found | 39,280 | 39,280 |
| unsupported | 1,479 | 1,479 |
| unknown | 969 | 969 |
| findings changed | | 195, all discovery not-tested to supported |
| wall time | 34 min 27 s | 80 min 19 s |
| CPU user | 12,517 s | 14,043 s |
| CPU sys | 9,686 s | 21,105 s |
| cores busy | 10.7 | 7.3 |

Every one of the 41,728 ports both runs assessed has the same outcome,
and the only findings that moved are the 195 ports whose maintainer's
own livecheck is now run and proven against the catalog, the
[overridden livecheck](2026-09-21-overridden-livecheck.md) work. Nothing
regressed. The sparse overlay's evidence holds over the whole tree.

## What the time went to

Kernel time doubled and parallelism fell by a third. One scoped overlay
measured 36 milliseconds to make and remove over the real `_resources`:
145 hardlinks, each a link to an inode that eight workers were linking
at the same moment. Every candidate evaluation and every modeled
observation made one, and a candidate evaluated and then observed made
two for the same contents.

Two changes from the morning's open list, both now landed: an overlay
that does not edit `_resources` points one symlink at the base's
directory rather than linking every file, and an input reuses the
overlay it already made for identical contents. The overlay measures 0.7
milliseconds.

The symlink had a consequence the fixture tests could not see. A
modeled observation classifies every read as inside or outside the
captured tree by its normalized path, and a PortGroup read through the
link normalizes into the base workspace, outside the overlay: 134 of the
devel category's 9,359 ports came back inconclusive, "filesystem state
outside the captured ports tree" at the `PortGroup` line. The observer
now knows the base root as well as the overlay's, since the base is the
same captured tree, and a test observes an overlay whose Portfile loads a
group through the link and fails without the change.

The devel category, 9,359 ports, was the check for both: with the
symlink alone, 134 ports inconclusive; with the observer's base root,
every port has the outcome and findings the whole-tree run gave it, and
the category run took 7 min 29 s at 0.46 CPU seconds per port, against
the whole-tree run's 0.84 and the baseline's 0.53, with kernel time down
from 60 percent of CPU to 45. The whole-tree figure is measured again by
the rerun the [next note](2026-09-22-survey-rerun.md) reports.

Eighteen ports in that category run report "missing indexed categories":
ports whose index entry carries no categories at all, obsolete ports and
one R port, which a category selection cannot place and reports rather
than guesses. The whole-tree run finds them by directory. That is how
category selection has always read the index, and it is noted on the
roadmap.
