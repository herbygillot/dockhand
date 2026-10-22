# The check pipeline, once

Roadmap part 1, item 7's first step (R3-4, R3-14). Queuing a check was
written three times, check's (command/check.go), ship's `queueHead`, and
`Engine.Ship`'s, with the policy for a check already active written three
ways.

`engine.QueueCheck` (engine/queue.go) is the one way now. It takes the
branch, the revision and plan a proposal made, and the origin, and:

- with `Covered`, takes a check that passed for these files and stands
  for this one (`Engine.Covered`) as its result, queuing nothing (ship);
- refuses a plan nothing can be built from with `UnrunnableError`,
  `engine.Runnable`, which check's own `--plan` refusal uses too, in
  place of command's `unrunnable`; an unresolved plan now names each
  unresolved port where check said "the plan is unresolved";
- meets the branch's one check queued or running as `Beside` says,
  judged before anything is queued: `BesideRefuse` returns the
  `ActiveRunError`, judged stopped or not (check, and the engine's ship);
  `BesideFollow` follows one of the same files whose plan covers this one,
  and refuses beside any other (ship); `BesideReplace` stops what holds
  the branch, asking `Stop` before stopping a running check (check
  --replace), and returns what it replaced and what each was before.

check words the refusal with its `--replace` hint (`besideWords`) and
says what it replaced (`sayReplaced`); command's `replaceActive` is gone.

Found on the way: an engine test reached the person's real Keychain
login through the engine's fallback GitHub runtime, used where nothing
gave the engine one, and failed when that login needed renewing. The
fallback now has no login (compose always gives the real runtime), the
engine no longer imports the Keychain, and engine fixtures get a forge
that knows of no pull requests unless the test gives its own.

Left of the step: `SeeThrough` deciding drive or follow from the lead
itself, and `Enqueue` returning the run it followed.
