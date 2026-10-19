# serve's major hold, and the hand branch, each decided once

Two more lines of the roadmap's v0.5.0 part 1 item 4, "One owner for each
rule", from the architecture review of 5897a82.

## R3-1: the serve path's major hold, once

`Engine.Ship` asked `goOn(shipEdit, ...)` whether a new major version held
an update nobody looks over, with outdated's versions, then `Update` asked
again with the release's, under the same `Unattended`, each in its own
words.

- `goOn`'s edit stage is gone, and with it `shipEdit` and
  `ShipTarget.Current`, which only it read. `Update`'s hold is the one, and
  `Ship` says its `HeldBeforeEdit`: "jq is held for a look before its edit:
  jq moves from 1.7.1 to 2.0.0, ...; no branch was started".
- What's left of `goOn` is the commit stage: `commitUnasked(options, tidy)`.
- The release outdated finds carries `CurrentVersion` (upstream's
  latest.go), which `Update`'s hold reads.
- Call sites switched: `Engine.Ship`'s two `goOn` calls.
- Grep for the old shape: `majorHold(` is read by `Update`, `majorHolds`
  (submission), and `HoldsMajor` (outdated's marks) only.
- Test: `TestShipAsksOnceWhetherItMayGoOn`, through `Ship`.

## R3-11: `NoHandBranch` decided in one place

The rc4 fix set `NoHandBranch` to `unattended || batch` at the command's
ship, while `Engine.Ship` hard-coded it true.

- `UpdateRequest.HandBranch()` decides it: a Start request keeps a branch
  for an edit by hand unless it's `Unattended` or `Batch`, the new field
  for one port's update among several.
- Call sites switched: the command's `shipOne` sets `Batch`; `Engine.Ship`
  sets `Batch: true`; `Update`'s `byHand` and the command's `byHand` words
  read `HandBranch()`.
- Grep for the old shape: `NoHandBranch` finds none left.
- Test: the `HandBranch` assertions in
  `TestAnUpdateStartsItsBranchOnlyForAnEdit`.
