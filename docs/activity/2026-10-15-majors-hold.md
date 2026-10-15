# The majors hold (v0.4.0 part 1, b)

Field testing at b8baa26b: `dockhand bump hyperfine` went from 1.21.0 to
2.0.0 and opened macports/macports-ports#35353 without a pause, where the
person's preference since 2026-10-06 is to hold off on major bumps. Built on
the typed holds, in the hold list serve's submit reads, so v0.4.0's holds and
`ship` read the same list:

- `holds.majors` (config `[holds]`): `hold`, the default, or `allow`.
  `dockhand config` and docs/usage.md list it.
- `engine.majorHold`, a concern from `model.FromHolds` with rule
  `major-version`: "jq moves from 1.7.1 to 2.0.0, a new major version, which
  holds.majors holds for a look".
- bump's update (`Unattended`) is held before its edit, from the release's
  current and new versions (`HeldBeforeEdit`), so nothing is downloaded or
  built; serve's preparation (`prepareOne`, origin serve) is held before it
  prepares anything; and a submission's concerns carry it, from what the
  branch's update edits recorded (`SubmitPlan.Majors`), for serve's submit.
- `outdated` marks an update it would hold, "a new major version, held for a
  look unattended" (`Engine.HoldsMajor`, where the decision lives).

Tests: TestAMajorVersionHoldsAnUnattendedUpdate, TestHoldsMajorsHoldsUnlessAllowed.
