# Verification target inference

## Scope

Committed combined bump/publication as `41aeefb`. Implement the approved target-omission workflow for tracked contributions: `verify` and `verify --branch <branch>`. This extends the human-edit path discussed in Claude's reviews while continuing to use explicit targets in the existing verification executor.

## Implementation

- Infer only the single target of an open tracked contribution. Keep its subport and variant choices, and apply explicit flag overrides. An explicit port selector retains its existing defaults and behavior.
- Check the entire selected snapshot against the recorded contribution base. Changes outside that port, absent scope, and changed evaluated target identity require explicit selection. This first rule is conservative after rebases that introduce upstream edits elsewhere.
- Keep the source snapshot fixed and use the existing branch/revision acceptance preconditions. Recheck the recorded inference target during acceptance even when the revision has not changed.
- Keep changed-path inspection in `git`, shared with checkout capture and publication. Keep the contribution-selection policy separate from evaluation and progression in `workflow/verification_target.go`.
- Show inferred selection and effective variants in the CLI's source summary. No inference or fresh lookup happens in the driver after acceptance.

All new code and tests are authored for v2. No v1 comments or tests were copied. No new package, dependency, or database migration is needed.

## Validation

The full `make test-race` suite, `make vet`, `CGO_ENABLED=0 make build`, and `git diff --check` passed. The built CLI help shows `verify [port]` and explains inference and overrides.

Workflow tests cover committed and working-tree input, frozen snapshots and idempotent retries, subport/variant inheritance and overrides, untracked/closed/detached contributions, zero or multiple targets, missing bases, changed names, edits outside the port, shared resources, untracked patches, and target changes before acceptance. Git tests cover both sides of moves, raw path names, mode changes, tree/commit inputs, missing objects, and cancellation. CLI integration uses real Git, SQLite, native MacPorts evaluation, and a fixture image: it exercises inferred evidence reuse from a named branch and current checkout, then captures a human edit with an explicit variant and fresh-verification choice into a durable job. No real VM or remote publication was needed.

## Limits

Inference requires an existing open contribution and a readable recorded base. It does not infer a target for an untracked branch from recent commits or jobs, nor expand verification to downstream dependents. Human rebases that bring unrelated upstream edits into the base comparison may require an explicit port. Image selection remains required under the existing configuration behavior.
