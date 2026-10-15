# Check and evidence: untracked files, gone and kept dependency archives (v0.4.0 part 1, c)

Three of part 1c's check-and-evidence items, from Codex's gcc15 group update
(2026-10-07) and the rc9 full stage:

- **An untracked file in a port the check builds is marked before the VM
  boots** (`engine.UntrackedInChanged`): "! textproc/jq/files/fix.diff isn't
  tracked, so this check builds without it: --include … takes it into this
  check, and git add … keeps it", beside the plain "Left out, not tracked"
  list. A new patch in files/ was left out of the check without a mark.
- **A dependency's kept archive that's gone is said, and its record dropped**
  (`driver.gone`, `store.ForgetDependencyArchive`, event `archive.gone`, which
  check prints and its JSON carries as altered archives are). Each check
  skipped it again in silence. The archives' records are read in the same
  view as the dependencies' (SQL-O5), and `KeepDependency` keeps the archive
  and records the dependency in one transaction (`keepArchive`'s `also`,
  SQL-S4).
- **A dependency's newest kept archive lasts until cleanup.after**
  (`newestDependencies` in the archive prune's keep set,
  `PruneArchives(before, dependenciesSince)`): the newest a guest installed
  each port from, in each environment, stays as a result's record does. The
  rc9 full stage saw kept archives go from 47 to 17 in a run, and rust built
  from source on a release with no MacPorts archive was built again the next
  day.

Tests: TestUntrackedFilesInChangedPortsAreSaid,
TestAGuestIsGivenTheDependencyArchivesAnEarlierGuestInstalled (the gone
archive), TestADependencysArchivesAreFoundByPortAndGoWithTheirArchives (the
newest kept until cleanup.after).
