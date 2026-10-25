# Hold policies, per port

2026-10-25, Then's item 2 (the reflections' scope item the person kept,
"except for hold policies"; the keys' names the person's "moves + after").
`[holds]`'s `majors` and `allow` held every port alike, where a
maintainer's way is per port.

- **config** owns the typed names: `Holds.Ports`, `[holds.ports."<port>"]`,
  each a `PortHolds` of `majors`, `allow`, `moves` (`MovesHold`,
  `MovesFollow`), and `after`. A value none of them takes is refused by
  name, and `after` naming the port itself too. `dockhand config` lists
  the keys each port gives.
- **compose** checks each port's `allow` against `engine.HoldRules` as it
  checks `holds.allow`, and hands the engine `engine.PortHolds`.
- **engine** composes a port's holds in one place, `holdsFor`: its own
  `majors` and `allow` over the global ones, its `moves`, its `after`.
  `majorHold` and `HoldsMajor` (now per port, as outdated marks it) read
  it, and `holding` sets apart each concern by its own port's allow list
  (`allowedFor`), where it read the global one.
- **moves = "follow"**: `Update` sets `FollowMoves` for an unattended
  update of such a port, the standing answer to the move's hold.
- **after**: `PlanSubmit` adds `afterHolds`, rule `after` (in `HoldRules`),
  holding the port's submission nobody looks over until each named port is
  at the same version on the master dockhand last fetched, or has a pull
  request open at it, found as the other open pull requests are. Not
  knowing holds, as with those.
- **Docs:** usage.md's configuration table.
- **Tests:** TestAPortsOwnHoldsAreRead (config),
  TestAPortsOwnHoldsReachTheEngine (compose),
  TestAPortsOwnHoldsGoOverTheGlobalOnes,
  TestAPortWaitsForThePortItsHoldsNameFirst, and the standing follow in
  TestAnUpdateSaysItsRepositoryWasRenamed (engine).
