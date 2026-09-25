# Independent review of the v3 implementation so far

2026-09-25. Reviewed `ba8f1eb8` on `main`, against the accepted
[Design v3](../design-v3.md), rather than treating the earlier Codex proposal
as an unchanged contract. This is a review, not an implementation change.

**The product direction is coming through clearly. Four reproduced correctness
gaps need attention before treating the new contribution loop as dependable.**
The first two affect the publication rules. The third affects recovery of staged
work. The fourth needs resolving before the multi-platform build path is relied on.

## What has landed

This is already a substantial rebuild, not a command-name experiment. The new
`model`, `store`, `coord`, `engine`, and `command` packages support branch/worktree
context, authoring without commits, captured revisions, commit tidying, submission,
queue execution, PR following, and the maintainer loop. The command and GitHub
providers have implementations. The v3 Tart and prefix providers are still pending;
the retained v2 Tart machinery and the completed SSH channel work are their
foundation, not evidence that v3's full Tart journey has run.

Several changes are particularly useful:

- Context is a branch/worktree choice, with an explicit `--branch` selector.
  A port name no longer silently chooses which contribution to edit.
- Captures and run plans have identities separate from mutable branches.
- `tidy` offers explicit squashing, reviewed grouping, saved plans, and checkpoints.
- The command layer's dependency check and recent engine extractions address
  responsibility boundaries instead of simply moving every verb into a subpackage.
- Passing-branch selection and result wording now have shared definitions.

The accepted choices to say "branch" on screen, allow `wait` to drive unattended
work, rebuild into a fresh v3 database, and support explicitly authorized automatic
submission are design decisions recorded with the user. They are not findings
against this implementation just because they differ from the earlier proposal.

## Findings

### 1. [P1] A focused check authorizes submission of untested changed ports

Location: [publicationProblems](../../internal/engine/evidence.go), lines 95–105;
also `PlanCheck`'s narrowing and `PlanSubmit`'s evidence selection.

`PlanCheck --only` removes the other targets from `Plan.Targets`.
`EvidenceFor` then selects a finished run by matching its source tree, and
`publicationProblems` checks only failures among that run's targets. It never
compares the evidence against the branch's complete required coverage.

Reproduced with an ordinary committed branch changing both `jq` and `libharbor`:

1. Check only `jq`, successfully.
2. Plan a normal submission, without `--draft`, `--no-check`, or `--accept`.
3. The plan reports both changed ports but has no submission blockers.

The probe prints:

```text
changed ports=[libharbor jq]
checked targets=[jq:substantive:changed]
blocking=[]
```

The PR's coverage table likewise has no row explaining the omitted port.
This directly contradicts Design v3 §7's rule that narrowing a check must not
quietly shrink submission requirements. A later focused pass can also supersede
the more informative result of a broader check on the same tree.

**Recommendation:** represent required coverage independently of the last run's
selection. Judge evidence against that coverage, with explicit missing results,
and report unchecked changed targets in the PR. Apply the same predicate to
ordinary submission, `--passing`, and serve's automatic candidates.

Regression probe: `TestReviewNarrowCheckDoesNotAuthorizeUncheckedChangedPort`.

### 2. [P1] Changes to shared build code can still receive revision-only treatment

Location: [targetKind](../../internal/engine/plan.go), lines 181–200.

The classifier considers auxiliary changes only inside the target's directory,
then compares its Portfile with revision lines removed. It does not account for
changed shared code under `_resources`.

Reproduced with a Portfile that loads the GitHub PortGroup: its own change is
`revision 0` to `revision 1`, while the loaded PortGroup gains a failing
`post-destroot` hook. Planning classifies the target as `revision-only`, and
`Acceptable` returns true. Thus a failure arising from the substantive shared-code
change can use `--accept`, even though v3 reserves that path for revision-only
targets and extras.

The probe prints:

```text
target=jq kind=revision-only acceptable=true
```

**Recommendation:** include relevant shared source when proving revision-only.
Until the evaluation observation machinery can establish the precise affected
set, conservatively treat uncertain shared-code changes as substantive. This
does not require finishing the entire oracle first.

Regression probe: `TestReviewSharedBuildCodeIsSubstantive`.

### 3. [P2] Tidy's checkpoint cannot recover a distinct staged version

Location: [ApplyTidy](../../internal/engine/tidy.go), lines 624–649;
`Restore`, lines 694–698; and `model.Checkpoint`.

A checkpoint records only the previous and new commit IDs. Applying tidy resets
the index to the new head. Restoring resets it to the previous head. The original
index tree is never saved.

Reproduced by staging one version of a Portfile, then editing the working file to
a different version before tidying. After `tidy` and `restore`, the working version
remains, but the staged version is gone from the index and absent from the
checkpoint:

```text
index before:   version 1.8.1, followed by "# staged version"
index restored: version 1.8.1, without the staged change
```

This is more than losing staged-versus-unstaged labels: an index can contain
content present in neither HEAD nor the working file. Its blob may remain
temporarily as an unreachable Git object, but `restore` has no recorded identity
with which to recover it. Design v3 §8 explicitly promises preservation of captured
index and working state.

**Recommendation:** capture and retain the pre-operation index tree, keep its
objects reachable, and restore it with checks against intervening changes.
Saved tidy plans should also detect an index change where applying the plan
would otherwise discard newly staged work.

Regression probe: `TestReviewTidyRestorePreservesStagedContents`.

### 4. [P2] The first environment determines dependencies for every environment

Location: [PlanCheck's candidate collection](../../internal/engine/plan.go),
lines 87–95.

The planner evaluates each environment, but once a port name is in `seen`, it
skips collecting that port's dependencies from subsequent environments. All
environments therefore inherit the first environment's dependency graph.

Reproduced with a consumer that requires the changed `libharbor` only on x86_64,
and a plan asking for arm64 first, then x86_64. With `--only harbor-viewer`, the
result contains just `harbor-viewer`; the changed library is missing entirely.
Reversing the environments would change which dependencies are seen.

This undermines the guarantee that selected consumers receive their changed
prerequisites, and can give the wrong ordering or blocked-target behavior. The
probe uses a platform-sensitive stand-in evaluator; it is not a claim that an
actual v3 Tart build has already exhibited the failure.

**Recommendation:** retain dependency relationships per environment and build
each environment's required closure and order from its own evaluation. A global
union is conservative for some cases but can manufacture cycles from relationships
that never coexist on one platform.

Regression probe: `TestReviewLaterPlatformKeepsChangedPrerequisite`.

## Verification and limits

The reviewed commit was exported into an isolated temporary directory. The
existing selection below passed on this Mac:

```sh
go test ./internal/model ./internal/store/... ./internal/coord \
  ./internal/engine ./internal/command
```

The engine and command suites finished in approximately 54 and 71 seconds,
respectively. The tests needed local loopback access for their mock HTTP servers;
the first sandboxed attempt stopped at that environmental restriction. The
subsequent run with loopback access passed.

Four additional focused regression probes assert the accepted behavior and fail
at the four points described above. Their source is preserved in the companion
[regression probe patch](2026-09-25-v3-regression-probes.patch). To reproduce, apply
it to an isolated copy of `ba8f1eb8` and run:

```sh
go test ./internal/engine \
  -run '^TestReview(Narrow|Shared|TidyRestore|LaterPlatform)' -count=1 -v
```

The probes use local synthetic repositories, a stand-in port evaluator, and the
existing fake provider/forge. They do not contact the real forge or build ports.
They are supplied as a review artifact, not installed in the production test suite.

This pass concentrates on the new contribution lifecycle and its critical
boundaries. It is not a line-by-line review of every changed package or a full
end-to-end MacPorts/Tart certification. Deferred providers, fine-grained artifact
reuse, and the oracle remain explicitly unfinished work, rather than defects
merely because they have not landed yet.

## Recommended next step

Fix the publication coverage and classification gaps, make tidy's checkpoint
complete, and preserve platform-specific prerequisites. Then use the v3 Tart
provider to prove the ordinary one-port path and a two-directory branch with a
changed dependency, including interruption and correction after a failed build.

The broad command surface is already present. Proving those few cross-command
guarantees is now more valuable than adding further convenience commands.

Only this review and its regression patch were added to the shared repository.
Implementation files were not modified, and no commits or pushes were made.
