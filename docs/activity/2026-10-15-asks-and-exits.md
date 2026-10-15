# How a command asks and exits (v0.4.0 part 1)

The command package review's concept 4 and its exit codes, the second batch
of part 1 (the roadmap's "How a command asks and exits"):

- **Exit constants** (`exit.go`): `ExitFailure`, `ExitCheckFailed`,
  `ExitAttention`, `ExitInterrupted`, at every site that chose a bare number;
  check's baseline reads `ExitCheckFailed` rather than branching on 2.
- **One consent value** (`consent.go`): `Streams.decide(yes, decision)` takes
  `--yes`, asks on a terminal, and otherwise refuses in one sentence,
  "nothing was removed: without a terminal, --yes removes these", exit 1. It
  says how it was answered (`AnsweredAsked`, `AnsweredYes`,
  `AnsweredNobody`), the fact holds and `build.unattended` will read. A
  question with a safe way on gives its answer where nobody is there.
- **The refusals folded**: clean's and `review --pr`'s, which exited 0 with a
  line on standard output, now refuse with exit 1, as outdated's, submit's,
  and `check --variants each`'s, which read alike now. The yes-or-no asks
  with a safe way on, stopping a running check and asking a reviewer again,
  go through `decide` too; the open questions (a category, a commit's
  subject, a menu) stay `ask`'s, and the code `ship` deletes is left for it.
- docs/usage.md says the refusal and its exit.

Tests: TestADecisionSaysHowItWasAnswered; the clean and review tests now
expect the refusal where a script previews without `--yes`.
