# 2026-09-27: one JSON shape per command, wherever it stops

The person's rule, revising the one recorded an hour earlier:
- A command's `--json` result has the same shape whatever point it stops at, whether a phase or an exit, so someone writing a tool reads one shape per command.
- The result is a superset of what the command's steps and exits can say. What doesn't apply at the point where it stopped is left out or empty. This is best effort.
- Different commands needn't share a shape.

The earlier ruling let a multi-step command report a result of whichever step it stopped at, with the exit code to tell them apart. A tool would then have had to guess the shape from the exit code.

**What didn't hold one shape.** An audit of every `emit` in the command layer found four:
- `submit --check` reported the submission's preview, then the check's result, then the submission's. A failed check left a check-shaped result under `"command": "submit"`, and a successful one dropped the check's results.
- `check` with `check.baseline` reported the baseline's result in place of the failed check's after a failure. The two couldn't be told apart, since a run didn't say it was a baseline (the code-organization review's finding 10).
- `create`, interrupted once the port was written, reported `null`, though a port exists and a retry is refused (finding 33's first part).
- `update` and `bump`, when dockhand can't make the edit and keeps the branch it started for the person, reported `null`.

The rest emit one type throughout, filled as far as they got.
- Plain `submit` fills `pull_request` once it submits.
- `tidy` fills `applied`.
- `update --submit` and `bump` already gathered their steps.

Commands whose result follows the arguments, such as `update --outdated` against `update <port>`, or `explain` against `explain <code>`, are one shape per invocation, which a tool chooses.

**What changed.**
- **One mechanism.** `Streams.linkSteps` takes the command's result as a `gatherer`, and each later step's `emit` is gathered into it. `updateJSON`, `submitJSON`, and `checkJSON` are the three.
  - A command already gathering keeps its own. So `update --submit`'s check stays under `update.check`, as before, when `submit --check`'s steps run inside it.
- **`submit --check`.** Its result is the submission's, with `check` inside when it ran one.
  - A failed check leaves `pull_request` null.
  - A refusal before checking, such as a failed check that stands for the same commit, reports the preview with its `blocking`.
- **`check`.** Its result is the check's, with the `baseline` that `check.baseline` runs inside it. A run's JSON gains `baseline_of`, the ID of the check a baseline looks into, which `wait`, `retry`, and `check --baseline` report too.
- **`create` and `update`/`bump`.**
  - `create` reports what it created when interrupted.
  - `update`/`bump` report the update with the branch kept for the edit by hand.
- **A refusal before anything happens** still reports only its error, with a null result.

**Tests.** `TestSubmitCheckReportsOneShape` covers a failed check, the refusal after it, and a passing check on a second branch. `TestCheckReportsItsBaselineInsideIt` covers a failed check with its baseline inside. Both fail without the gathering. The by-hand test gains a `--json` case naming the kept branch. `create`'s interrupted path isn't tested: interrupting it needs a canceled context in the middle of its checksum refresh, which the tests' fakes can't give.

**Docs.**
- The roadmap's Decided entry is rewritten.
- The usage guide's Scripting section and design §12 state the rule.
- The code-organization review's finding 10 is now declined on this rule: one shape per command, not one across commands.
- The baseline marker is done.
