# 2026-09-29: an excluded environment isn't called tested

The beekeeper-studio run ([review](../reviews/2026-09-28-hugo-bump-exercise.md#beekeeper-studio-a-port-that-needed-a-portfile-change), finding 4) found a pull request claiming a test that didn't happen. `platforms {darwin >= 23}` excluded macOS 12 from check-21, and its grid said so. But `submit --plan` said the check passed on macOS 12 too, and the pull request's Tested on listed "macOS 12 (Monterey) arm64 / Xcode, its version not recorded · tart: built in a clean VM", though nothing was built there.

Tested on went through every planned environment, and gave one with no provider runs an empty report of its own. Submit's plan joined every planned environment.

Now whether an environment was tested is the evidence's to say (`Evidence.Tested`): some result came from a provider run there.
- **Tested on** names only those environments: one with no runs has no report. The table still says "— excluded" where a port is.
- **Submit's plan** says the check passed on the environments it tested, and adds where nothing was built because every port is excluded there (`Evidence.ExcludesAll`): "passed on … for this commit's files (check-21); nothing built on macOS 12 …, where every port is excluded". A failed check's line is as it was.

Why a port is excluded, which said `known_fail` of beekeeper-studio's `platforms`, is the run's finding 3, with build eligibility in item 6.

`TestAnExcludedEnvironmentIsNotCalledTested` pins Tested on's whole text and the table's cell, and `TestSubmitsCheckLineNamesOnlyWhereItBuilt` submit's line. Seven mutations each fail a test.
