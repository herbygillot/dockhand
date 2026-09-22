# Architecture follow-up — 2026-09-22

Reviewed commit: `992c3ab9969dedbce488da7a76fa551106217990`  
Comparison commit: `0f201ebdf477ef1bbf47d8b01213cba1a3f999f4`

This follow-up reviews the production code and CLI behavior after the previous architectural review. Documentation and tests were excluded as architectural evidence. Code links are pinned to the reviewed commit.

The architecture has improved materially. The largest improvement is that preparation onto an existing contribution now runs through the durable workflow. The remaining weakness is **preserving the contribution's meaning as that shared pipeline replaces its revision**. Three concrete problems occur at that boundary.

1. **[P1] Release-scope validation can fail after the branch has already moved.**

   The new preparation path uses `result.Scope` as the replacement revision's scope. But that describes the latest edit, whereas the existing revision's scope records the whole contribution.

   For example, a checksum refresh onto a shared-release contribution produces a result with no scope. Integration replaces the Git branch, then SQLite rejects the new revision because its scope does not match the previous revision. The database transaction rolls back; the Git update does not. Retrying encounters the same incompatible candidate.

   The relevant chain is [candidate construction][candidate], [branch replacement][replacement], and [revision validation][revision-validation]. The checksum editor explicitly starts with an unscoped [result][checksum-result].

   The previous correction path rebound the existing scope before integration. That distinction needs to survive the unification: preserve and revalidate contribution scope for corrective edits, define an explicit policy for scope-changing updates, and check compatibility **before moving the branch**.

   > **Response, 2026-09-22.** Confirmed in the code, and it bites on two of the three preparing actions: the version bump carries a scope from its release, but a checksum refresh and a revision bump produce none (`portedit` sets `Result.Scope` only in `version.go` and `git_source.go`), so either onto a shared-release contribution fails the membership check after the branch moved. Two points of disagreement. First, the consequence is worse than "retrying encounters the same incompatible candidate": after the rollback the branch head is the new commit while the records still name the previous revision, so the next bump onto the contribution binds against a `PreviousHead` the branch no longer has and is refused as a moved branch; the contribution is left inconsistent, not merely stuck, and only a hand reset of the branch or an adoption recovers it. Second, no new policy for scope-changing updates is needed: the store already has one, `PutRevision` refuses a revision whose membership differs from its predecessor's, and a contribution's scope is fixed at its creation by design. What is missing is that rule at binding: an update onto a contribution rebinds the prior revision's scope onto its candidate, as the correction path does, and is refused there when the membership would change, so the branch never moves for a revision the store will not record.

2. **[P2] Preparing onto an attached PR loses its publication destination.**

   `bindOnto` reads the attached PR to check its state and remote head, but does not retain the PR itself. `BindPreparation` subsequently resolves publication through the checkout's remotes.

   For a maintainer updating an adopted PR from somebody else's fork, this can select the maintainer's fork. Preparation and verification can succeed, only for publication to reject the result with `existing PR destination cannot change`. The publication guard prevents publishing to the wrong destination, but the request was bound incorrectly much earlier.

   See [the discarded PR context][pr-context], [destination selection][destination], and [the eventual rejection][destination-rejection].

   The existing `publicationDestinationFor` already implements the right policy. The unified preparation path needs to supply the attached PR to it.

3. **[P2] Preparing onto a contribution drops its existing commit body and ticket trailers.**

   `ContributionSubject` extracts only the first line of the existing message. The new durable path then creates a completely generated message using `intent.Message()`. Consequently, an adopted or amended contribution containing explanatory paragraphs, `Closes:`, or `See:` trailers loses those unless the new request supplies them again.

   Previously, onto preparation passed through `BindCorrection`, which preserved the existing message and applied `portedit.Rewrite`.

   See [subject extraction][subject], [new message construction][message], and [the existing preservation logic][rewrite].

   Commit composition should explicitly distinguish creating a contribution from revising one.

These are related architectural findings. Preparation has been unified at the **execution** level, but contribution revision semantics remain divided between `BindCorrection`, `bindOnto`, and `prepareCandidate`.

A small contribution context or revision plan should own the prior revision, accepted scope, message-preservation policy, attached PR, and replacement preconditions. The immutable source commit can supply the original message; it need not be duplicated in storage. The important part is giving these decisions one owner and using it for both manual corrections and generated updates.

Several earlier findings can now be closed or substantially downgraded:

| Earlier concern | Current assessment |
|---|---|
| Onto preparation happens outside the durable job | **Substantially addressed.** Release resolution and candidate preparation now use the checkpointed workflow. Patch findings and `AllSubports` are carried through. |
| Git subprocesses run under SQLite write transactions | **Addressed at the identified sites.** Shared-file discovery now precedes the write transactions in submission, integration, and reassociation. |
| Workspace behavior depends on a global root registry | **Addressed.** [`Tree.Projection()`][projection] makes materialization capability explicit. |
| Archive passes duplicate coverage bookkeeping | **Addressed at the right scale.** [`archiveCoverage`][coverage] centralizes declaration coverage and download deduplication while leaving each pass's acceptance rules local. |
| Retention and branch cleanup lack a clear owner | **Meaningfully improved.** [`retention.Collector`][retention] has explicit dependencies, and lifecycle cleanup and sweeping share `DeleteLocalBranch`. |
| Provider-selection policy lives in application wiring | **Improved.** `workflow/choice` owns fallback and configuration policy; `app` supplies the providers. |

The retention extraction is particularly good. It owns collection decisions while delegating claimed, recoverable resource release back to the workflow. It does not require receiving the entire `Engine`. That is a useful pattern for subsequent extractions.

**`workflow` remains the main concentration of responsibility.** Its direct production Go code grew from 7,171 to 7,308 lines, even after retention moved out; `app` fell from 1,692 to 1,460. These counts include comments and blank lines and exclude tests and child packages.

The growth is not itself a problem—moving workflow policy out of `app` is appropriate. But `Engine` still owns selection, continuation, adoption, correction, intake, execution, PR lifecycle, and state projection. The next meaningful boundary is contribution management: deciding what contribution exists, how it may change, and what must be preserved across revisions.

Establish that owner inside the package first, then extract it once its dependencies are narrow. Moving methods into another package while giving that package the whole engine would leave the coupling intact.

The new `Resolution` is useful, but its contract needs tightening before more callers depend on it.

[`ResolutionRequest`][resolution-request] combines action selection with adoption, squash, body preservation, required lookup, lookup-only behavior, preview, and offline behavior. Meanwhile, `Continue` has two meanings:

- Preparation continues a prior job's frozen input and inherited choices.
- Verification or correction selects a tracked contribution's current revision, potentially without any prior job.

Those are distinct results with different guarantees. Verification currently consumes essentially the `Change` from this much larger value, then performs its own binding.

There are already signs that the mode combinations are difficult to maintain: the nil-store shortcut bypasses the `Lookup` and `Require` handling, while `Lookup`, `Require`, and `Offline` have no production callers. Adoption also makes `Resolve` a mutating operation in one mode. See [the dispatch][resolution-dispatch].

Keep `Engine.Resolve` as an orchestration entry point if that remains convenient. Internally, separate a **selected contribution snapshot** from a **preparation decision**, with explicit payloads for fresh preparation, continuation, and revision replacement. That would also provide a natural home for the context missing in the three findings above.

`portedit` is healthier, but the earlier lifetime/ownership concern remains.

The coverage extraction removes real duplication. However, [`sourceInput`][source-input] still combines workspace and interpreter lifetime, shared overlay caches, baseline contents, evaluated metadata, observation state, and evolving release scope. [`dependencyBase`][dependency-base] still creates another interpretation of that object through a shallow copy followed by selective field replacement.

The new overlay cache makes shared ownership more consequential. The next change here should be two internal types:

- An editing session owning interpreters, overlays, caches, and cleanup.
- A baseline or candidate value containing contents, snapshot, target, and source interpretation.

Derived baselines could then share a session explicitly. Make that separation before creating another package.

Two previous concerns remain unchanged and are lower priority than the contribution defects:

- [`Cycle`][cycle] still performs potentially long operations sequentially, delaying other observations and controls. Any bounded concurrency work must first remove the cycle's mutable “current provider” fields from individual operations.
- [`updateExecution`][execution] still discovers persistence changes through shallow copies and `reflect.DeepEqual`, with record ordering embedded in the writer. Explicit transition results would make mutation ownership easier to reason about.

Address the three revision-preservation defects first, then tighten the contribution/resolution model. The workspace, coverage, and retention changes are worth keeping as they stand.

Validation: built the exact reviewed commit successfully and inspected `dockhand help`, `dockhand help bump`, `dockhand help verify`, and `dockhand help publish`. The behavioral findings come from tracing production code; no tests or live verification/PR-publication workflows were run. The review made no production-code changes.

[candidate]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/workflow/preparation_run.go#L192
[replacement]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/workflow/preparation_integrate.go#L183
[revision-validation]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/state/sqlite/records.go#L198
[checksum-result]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/macports/portedit/checksums.go#L20
[pr-context]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/workflow/onto.go#L69
[destination]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/workflow/preparation_bind.go#L139
[destination-rejection]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/publish/plan.go#L178
[subject]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/workflow/onto.go#L20
[message]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/workflow/preparation_run.go#L198
[rewrite]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/macports/portedit/message.go#L94
[projection]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/macports/context.go#L20
[coverage]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/macports/portedit/coverage.go#L18
[retention]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/workflow/retention/collector.go#L43
[resolution-request]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/workflow/resolution.go#L80
[resolution-dispatch]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/workflow/resolution.go#L122
[source-input]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/macports/portedit/source.go#L27
[dependency-base]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/macports/portedit/dependencies.go#L126
[cycle]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/workflow/cycle.go#L91
[execution]: https://github.com/herbygillot/dockhand/blob/992c3ab9969dedbce488da7a76fa551106217990/internal/workflow/execution.go#L92
