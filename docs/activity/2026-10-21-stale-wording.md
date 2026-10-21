# Stale wording from the second dead code scan

Group D and C12 of the dead code scan of 9bdc25c
(cleanup/dead-code-scan-2026-10-21.md), at the person's word and as the
coordinator listed them. Wording only, with no behaviour change. The
scan's other groups are left for the person to decide.

- Messages, and the tests that pin them:
  - history.Unfinished names `undo`, not restore.
  - checkOutAgain's refusal for an archived branch with nothing left says
    `dockhand start <name>` offers on a terminal to rename it aside,
    since b717dbad did that. Before, it said reusing the name came after
    the release (C12).
  - depblock's missing go2port or cargo2port says to install it on PATH,
    where it named a `--go2port` flag dockhand no longer has.
- Comments and docs:
  - PlanClean's doc names closed branches, and archived ones cleaned by
    name.
  - PrepareOutdated's names `To: ShipCheck`.
  - ship's doc says it submits nothing.
  - updateJSON's names ship and bump.
  - providerLines' names setup.
  - usage.md says `init`, `providers`, and `auth` are gone.
  - The Makefile's mutate example drops `TestLegacy`.
  - F11's comment drops `update --check`.
- Tests:
  - "submit --passing" reads `ship --passing`.
  - TestSubmitCheckPassingAndReady is now TestShipPassingAndSubmitReady.
  - TestPassingIsOneDefinitionForSubmitAndServe is now
    TestPassingIsOneDefinitionForShipAndServe.
  - work_test says setup refuses.
  - The worktrees assertion names the fixture's setting, and
    `DefaultWorktrees` is asserted beside it.
