# 2026-09-30: results checked as written, and an identity that can't be read

Item 6's fifth piece is results and the reuse predicate. This note covers two of its three parts, the code-organization review's findings 28 and 39. The third, finding 25's kind on each cell of the evidence, follows in its own commit.

## Every result's vocabulary, where it's written (finding 28)

Every result goes through `tx.RecordResult`, which runs `TargetResult.Validate`. That checked the outcome, and that a failure names a phase, but not which phase, nor the tests outcome. The script provider checked both itself, while Tart checked neither. A reused result is written again under the new execution, so a word no reader knows would be carried on by reuse, not only recorded once.

Now:
- `Validate` checks the phase, where one is named, and the tests outcome, where one is given, against their vocabularies.
- The same checks cover each builder's part of a GitHub result.
- `model.Phase.Valid` and `model.TestOutcome.Valid` hold the lists, which the script provider now calls rather than repeating them.

A result that says nothing of its tests stays valid: not run, blocked, and interrupted results have none.

## An identity that can't be read fails the attempt (finding 39)

Before each attempt, the runner read the environment's identity through `identitiesNow`, which dropped a read that failed. So the execution recorded "", which is what a provider that can't say records. Evidence judges a result by comparing the identity it recorded with the environment's now, and a recorded "" against a later identity read as the image made again. So a Tart image record that couldn't be read once, while the image worked, made every result of that check stop counting later.

Now:
- `identityNow` returns the read's error.
- The runner records an attempt that couldn't read the identity as an infrastructure failure, "couldn't read what the environment is: …", and builds nothing on it. The next attempt reads it again, and attempts run out as any failing environment's do.
- Judging evidence still treats an identity it can't read now as one the provider can't say. The results then stand, rather than every status failing on one unreadable record.
- `logs --json` gives each execution's recorded identity, so what evidence compares can be seen.

## Tests

- `TestTargetResultCheckpoints` covers each phase and tests outcome, words outside each, and a builder's part.
- `TestAnIdentityThatCantBeReadFailsTheAttempt` covers one failed read, then a finished attempt with the identity recorded and one build. It also covers reads failing on every attempt, which fails the check.
- `TestLogsSayWhatTheEnvironmentWas` covers the JSON field.

Seven mutations each fail a test.
