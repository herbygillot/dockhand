# 2026-09-22: the offline promise of assess is enforced, and the mirror is configurable

## What was open

The usage guide says `assess` and `outdated` stay offline, and the index
cache's mirror bootstrap is gated on it: the reader those commands build
carries no mirror. Nothing pinned that. A change that handed them a
mirror would have passed every test, because the mirror's address was a
constant in `portindex` and no test could observe a request to it.

The test suite had the opposite problem. Every command that goes online
seeds a cold cache from the real mirror, and the seed asks the mirror
before it checks whether the commit can be bracketed, so each test
binary's first bump made one request to ftp.fau.de. The suite depended
on the network without saying so.

## The change

The mirror's tarballs directory is configurable: `Config.IndexMirror`,
then `DOCKHAND_INDEX_MIRROR`, then the default, resolved in one place in
`app` and handed to the four readers that go online; assess and outdated
still get none. `portindex.MirrorURL` names the platform's index under
whichever base is given.

The assess test points the variable at a local server that counts
requests and asserts, beside its existing zero counts of forge and
archive traffic, that a cold cache took no seed from it under the
default assessment and under `--at`. The three test mains that run
commands point the variable at an unreachable local address, so the
suite never reaches the real mirror.

## What the unreachable mirror showed

Pointing the test mains at an unreachable address turned every cold-cache
bump in the suite into a failure: the seed's fetch error was returned as
the command's. The mirror is a seed, not a requirement, so a fetch that
fails, or a mirror without an index for the platform, is now reported and
the index built in full, which is what happens for a commit the mirror
cannot bracket anyway. A test asks a mirror that refuses the connection
for a seed and gets a full generation.

## Not closed on the way

The open list of 2026-09-22 carried "`patches.go` checks `filespath`
containment against the wrong root". Read again under scoped overlays, I
closed it: the readers of `filespath` read files at the evaluated path,
which names the projection the evaluation ran in, and an overlay holds
the port's whole directory. That reading was wrong, and the survey said
so within three minutes of starting: every Go port with a local patch
came back unsupported, "patches must be inside the frozen port
directory", where the baseline had found its input. The derived
baseline that the dependency path evaluates in an overlay was handed
the workspace's port directory at two sites in `dependencies.go`, the
sources policy and the patch reading, and the containment check
compared an overlay's `filespath` against the workspace's port
directory. Both sites now use the projection the baseline was evaluated
in, the refusal names both paths, and the Go dependency test has a
scenario with a real patch under `files/`, which fails without the fix.
The [survey note](2026-09-22-survey-after-workspaces.md) has the rerun.
