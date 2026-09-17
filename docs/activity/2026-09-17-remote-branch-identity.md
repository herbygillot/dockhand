# One remote branch per contribution after a rename

The roadmap's GitHub verification follow-up: after `reassociate` moved a contribution to a renamed local branch, GitHub verification pushed the new local name to the fork while publication kept updating the PR's original head branch. One contribution then owned two fork branches, and workflow evidence recorded on the new name could not be matched against the head branch publication compares.

## Design

`record.BuildSpec` gains `RemoteBranch`, the fork branch a forge verification pushes to and observes when it differs from the local `Branch`; `PushBranch()` returns whichever applies. The workflow sets it when planning verification for a change that already has a PR, from the PR's recorded head branch, and applies the same rule to the build it compares evidence against before publishing, both at binding and in the publication policy. The GitHub provider now locks, checks the expected remote head, pushes, matches workflow runs, and records workflow evidence on `PushBranch()`, while it still captures the commit from the local branch. Evidence applicability compares workflow evidence on the same remote branch. Tart verification is unaffected: it never pushes.

Nothing new is authorized: the fork branch was already the destination selected by `--provider github`, and for an unrenamed contribution the remote branch is the local name as before.

## Validation

- A workflow test publishes a contribution, renames the branch through `reassociate`, verifies on the new name, and checks that the attempt captures from `renamed` while its remote branch stays `candidate`.
- An applicability test shows workflow evidence recorded on the PR head still matches after the rename and does not match without the remote branch.
- The verify, GitHub provider, workflow, and SQLite suites pass; the remote branch persists with the attempt's build options.
