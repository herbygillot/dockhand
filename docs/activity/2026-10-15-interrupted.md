# A lost guest's target is interrupted

The 2026-09-25 model planned an "interrupted" outcome for a target that
was building when its guest was lost, and no provider recorded it: a lost
Tart guest's execution ended as infrastructure trouble, and the target it
was building read as not run, the same as one it never reached. The person
asked for it to be wired up rather than deleted.

- **The guest says what it is building.** guest.tcl writes the target's ID
  as `building` in results.json as it begins each target, and clears it
  with the target's result. That reports more and builds and judges
  nothing otherwise, so `VerifierProtocol` stays 3 and only the pin moves.
- **Tart records it.** When the VM stops, the guest can't be read for a
  minute, the program errors, or it stops before it finishes, the target
  the guest last said it was building is recorded as interrupted, with why
  the guest was lost as its detail. A guest that stops is read once more
  first, and what it finished in that last read is now recorded too, where
  before only a finished last read was.
- **It is no verdict.** The runner tries the target again, as before, and a
  dependency interrupted in an earlier attempt no longer blocks what needs
  it: `Blocked` and `blockRemaining` count only complete results. The
  evidence reads an interrupted result as a cell not run, with no execution
  behind it, so the target is unchecked, an earlier check's result fills
  it, and the lost attempt isn't listed among the runs behind the results.
  It reads "· interrupted" where every attempt was lost.

GitHub and the command provider can't say what was building when a run is
lost, so they record nothing new. `OutcomeUnevaluated`, which nothing
produced either, went the next day ([note](2026-10-16-bridges-and-unproduced.md)).

Tests: `TestTheGuestSaysWhatItIsBuilding`,
`TestTheTargetALostGuestWasBuildingIsInterrupted`, and
`TestAnInterruptedTargetIsBuiltAgainAndBlocksNothing`, which fails without
either the runner's or the evidence's change.
