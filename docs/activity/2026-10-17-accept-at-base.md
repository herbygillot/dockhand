# submit --accept takes a failure the base has too

Field testing at 26782c49: pgdog 0.1.62 failed at install on macOS 12,
`check --baseline` reported "✗ fails at the base too, at install", and
`submit` refused anything but a draft. A failure master has too is no
regression, the usual case for a MacPorts pull request. The person chose
`--accept` (2026-10-17, "Go as you recommend"), attended only.

- **engine `failsAtBase`** (evidence.go): the changed ports that failed,
  whose every failed cell a finished baseline of the check that made it
  (`Run.BaselineOf`) fails in the same environment at the same phase, each
  with those baselines' names. `SubmitPlan.AtBase` keeps them.
- **The gate:** `acceptanceProblem` takes `--accept` of such a port, and
  still refuses one the base doesn't fail alike, now naming the baseline as
  the way to see. `publicationProblems` says it: "jq did not pass in
  check-1, and fails the same way at the base (check-2), so not by this
  branch; --accept jq submits it, saying so, or fix it, or share it as a
  draft (--draft)". A changed failure with no such baseline points to
  `check --baseline`.
- **The pull request:** a cell's accepted words say why
  (`acceptedWords`): "✗ failed at install, accepted: fails at the base too
  (check-2)", where an extra's still say the cause wasn't established;
  `targetWords` and `EvidenceWords` take the reason as a string.
- `bump` and `serve` pass no `--accept`, so they never take one; the
  roadmap's "Baseline results stay in the check's output" (2026-09-27)
  gives way, for an accepted failure, to naming the baseline in the pull
  request.

TestAFailureTheBaseHasTooIsAccepted (alike, at another phase, passing at
the base); TestSubmitFollowsThePublicationRule's changed-port wording;
TestTargetWordsAreDesignV3s.

## A port blocked only by accepted failures (2026-10-18)

Field testing at 56e328ea: py-textual-fastdatatable 0.19.3's py310 to
py314 subports each failed at install on macOS 12 (Xcode 14.0.1's
libgcc15), and the base failed each the same way; the stub,
py-textual-fastdatatable, was "blocked by a failed changed dependency".
`--accept` of the five was refused for the stub, and `--accept` of the
stub as a changed port with no baseline failure of its own.
`blockedByAccepted` (engine/evidence.go) takes, with the accepted, each
port that failed only as blocked, in each environment by failed
dependencies all of which are accepted, or blocked so in turn; the gate
passes it over, unnamed. A dependency not accepted still blocks it.
TestAPortBlockedOnlyByAcceptedFailuresIsAccepted.

3cf14522 didn't clear py-textual-fastdatatable: on minmax's skipped
releases a cell is excluded, but its outcome reads not run, which
`blockedByAccepted` took as unbuilt, and `dependencyFailed` as a failed
dependency. Both now read the cell's kind, passing over an excluded one.
The test now has check-416's shape, a built oldest and newest release and
one left out between; against a copy of the database, `submit --branch
py-textual-fastdatatable-0.19.3 --accept py310…py314-textual-fastdatatable
--plan` reads "ready to submit", the stub's cell "blocked by a failed
changed dependency" beside the five accepted.
