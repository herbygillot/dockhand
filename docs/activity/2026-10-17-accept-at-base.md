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
