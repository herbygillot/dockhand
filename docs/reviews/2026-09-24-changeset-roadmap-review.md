# Independent review of the changeset direction and roadmap

2026-09-24. Review of the current [roadmap](../roadmap.md) and the
[contracts direction record](2026-09-23-contracts-direction.md), especially
decisions 19–30 and 36–37. These are recommendations for discussion, not
additional accepted decisions. Implementation inspected at `3108db74`,
with the current uncommitted design documents included.

**The changeset should be Dockhand's primary unit of work.** It matches
what a person maintains, verifies, discusses in a PR, amends, and retires.
A library update and its consumers' revision bumps have one purpose and
one publication lifecycle. Treating those consumers as special appendages
of a primary port made that ordinary case harder to represent.

The decisions to derive scope from the diff, preserve the author's commit
structure, retain a stable change identity and branch name, and keep
shared releases as a preparation concern fit together well. The existing
`Change`, immutable `Revision`, and frozen `VerificationPlan` provide a
good foundation. This does not need a general workflow framework or a
replacement for all the existing domain types.

The main remaining risk is that “changeset” absorbs several distinct
identities. Keep their responsibilities explicit:

| Concept | Responsibility |
| --- | --- |
| Changeset | Stable identity, branch/PR association, title, lifecycle. |
| Revision | Immutable candidate source and base; source of the changed scope. |
| Verification plan | Frozen requested coverage, exclusions, dependency relationships, and environment choices for that revision. |
| Guest execution | One admitted run for a changeset and platform, with shared resource ownership and recovery. |
| Target result | What happened to a particular target/configuration with particular inputs; reusable only when those inputs remain applicable. |

A port remains an excellent editing target and command shortcut. A
changeset-level status should explain its aggregate state through the
target results, including exclusions and missing coverage.

**1. Define revision-only conservatively; metadata equality is insufficient.**

Decision 22 calls a target revision-only when evaluated metadata is
identical except for `revision`, with no `files/` change. That is an unsafe
predicate for granting the more permissive publication policy in decisions
20–21. Portfiles contain deferred executable code which is not represented
by the metadata Dockhand currently reads.

I checked this using the existing saved baseline probe and the installed
Base 2.12.6 evaluator. A synthetic Portfile was evaluated twice at the same
path. The second version changed `revision 0` to `revision 1` and changed:

```tcl
post-destroot {
    ui_msg before
}
```

to:

```tcl
post-destroot {
    error {changed build hook fails here}
}
```

Both evaluations succeeded. The only differences in `PortInfo` were
`Revision` and `Options/revision`. No build phase ran; this is a bounded
demonstration of the metadata blind spot, not an actual failed build or a
test of an implemented classifier. Raw outputs are in the temporary
`dockhand-changeset-review-ti9h_t_p` directory from this review.

Require a source-level justification that the effective change is confined
to revision declarations, including relevant shared code. A generated
revision bump can provide that justification by comparing the immutable
before/after edits. Adopted or subsequently amended code needs the same
check; provenance alone must not survive arbitrary edits. Unknown cases
should be substantive. Equal observed metadata can support the check but
cannot prove it. Shared declarations affecting sibling subports will need
an explicit rule, rather than a promise of arbitrary Tcl equivalence.

**2. Separate changed scope, requested coverage, and prerequisites.**

Decisions 22–23 specify most of the intended behavior, but leave a critical
composition case open. Suppose a changeset updates `libA` and `appB`, where
`appB` depends on `libA`, and the request is `--only appB`.

The verifier still needs the candidate `libA`, either built in this run or
materialized from an applicable archive. Installing an old binary because
`libA` was excluded from requested coverage would test the wrong candidate.
I recommend automatically including changed prerequisites, displaying them
in the plan, and propagating their failures as blocked dependent targets.
Alternatively, refuse a selection that cannot be satisfied; do not silently
substitute another revision.

The plan should distinguish:

- Changed directories/resources and their affected target candidates.
- CI eligibility per platform, retaining the reason for every exclusion.
- Explicitly selected coverage, including `--only` and `--also`.
- The prerequisites needed to execute that coverage against this revision.

Keep eligibility and dependency expansion platform-specific. A genuine
evaluation failure is an unresolved plan, not evidence that the target is
ineligible and can disappear. Selection is frozen at intake; the concrete
plan must remain bound to its revision and environment as it is resolved.

Also state how narrowing affects publication: “substantive targets must
pass” and “not built locally” need a shared definition of required
coverage. A selected substantive target failing is different from one the
user intentionally did not select. Preserve that distinction in status and
the PR, rather than giving both a blanket changeset-level “verified”.

**3. Per-target reuse needs observations and artifact identities.**

Decision 28 is right to move away from whole-repository identity. The
recorded-input argument, however, is stronger than its proposed record.
Two additions are necessary before enabling this by default.

First, a file's absence is an input too. Consider an unchanged Portfile:

```tcl
if {[file exists $helper]} {
    source $helper
}
```

Adding the previously absent helper changes evaluation without changing
any previously sourced file. Directory/glob membership, failed name
lookups, and symlink resolution have similar properties. Record and
validate the relevant queries and their answers, including negative
answers, alongside file content identities. Registry observations also
need to account for whatever the query actually observes, not assume that
the active package set describes every possible installed-package query.
Passing through the oracle's dispatcher is an opportunity to record an
observation; it does not itself make the reuse manifest complete.

The guest-side re-evaluation check helps when a guest runs, but cannot
protect the all-reused path, where the host decides alone. Retain a
conservative invalidation rule wherever observation coverage is incomplete.
Including all `_resources` helps that subtree; it is not a general
substitute for recording the rest of the observations.

Second, a dependency's name/version/revision/variants and “built here”
label do not identify the bits used. An amend can change `libA`'s patch
without changing those coordinates. `appB` must not reuse its previous
result merely because its dependency still has the same package version.
Record the producing input identity and the exact consumed archive digest;
carry the corresponding identity through dependencies. The existing
`Artifact.Digest` and `BuildSpec.Inputs` are useful foundations.

Distinguish the identity of a build request from the digest of its output.
Two executions can have identical declared inputs and different bytes.
Even within the accepted limits on reproducibility, the result should say
which output was actually consumed.

Evidence validity and archive availability are also different facts. A
passing result can remain valid after an archive is unavailable, but it
cannot satisfy a later target's installation requirement until the
artifact is restored or rebuilt. Make archive transfer/checksum completion
durable before treating an output as ready for dependents. Cleanup must
respect every live reference to shared artifacts, not merely retirement of
the changeset which first produced them.

**4. Give the shared guest an explicit lifecycle.**

One guest per changeset/platform is a substantial change to the current
execution contract: `record.Attempt` and `BuildSpec` describe one target,
while admission, cancellation, and recovery attach provider runs to those
attempts. Putting several targets in that guest changes resource ownership,
not only the guest command loop.

There should be one owner of the guest execution and durable target
checkpoints beneath it. A failed or canceled run must preserve completed
results, distinguish a target that failed from one interrupted mid-build,
and leave blocked/not-started targets explicit. Retry must consume only
complete applicable results and available artifacts. The exact types can
be settled in the existing workflow/provider boundaries; avoid giving
several independently leased target attempts ownership of the same VM.

This contract needs to be settled before implementing target-level retry
and reuse. It is also the right place to define how stale messages from a
superseded guest are rejected.

**5. Baselines support a narrower statement than the current wording.**

Decision 20 identifies an already-broken target by a failure in the same
package and phase on the base revision. That is useful evidence, but two
different compiler errors in the same package both fail its build phase.
Likewise, one passing baseline does not by itself establish causation for
a flaky failure on the candidate.

Report the observation precisely: “also failed on the base revision in
the same package/phase”, linking both results. If automatic publication
depends on asserting the same failure, use a suitably conservative failure
comparison or require acknowledgement when the cause is uncertain. The
existing acknowledgement wording, “cause not established”, is good.

**I would adjust the implementation order.**

The immediate defect fixes, reliable guest channel, host-independence
foundation, and Base boundary deserve their current priority. The material
sequencing issue is roadmap item 6 implementing per-target reuse and retry
before item 7 establishes changeset execution.

Bring the minimal changeset and coverage contracts forward, even while the
evaluator work proceeds. Then deliver a narrow complete path: adopt a
two-directory branch, freeze its plan, verify in one guest with per-target
checkpoints, and publish accurately described coverage. Conservative
whole-tree invalidation is acceptable for that first path. Add dependency
archive retention and carefully scoped reuse after those identities and
checkpoints work. Per-target reuse matters greatly for large changesets;
it need not be the entry requirement for validating the new model.

The broad oracle work should remain incremental. A changeset whose
requirements are supported can exercise this path before every uncommon
environmental observation is modeled. Unsupported cases should remain
explicitly unsupported, rather than acquire guessed answers.

Follow with preparation conveniences, multi-port revision-bump selection,
`drop`, and richer command composition. Avoid implementing large amounts
of reuse against the old one-target-per-guest structure and then moving
them again.

Before the schema changes, consolidate the accepted changeset contracts
into one current design section. The decision log is valuable history,
but several decisions amend earlier ones; it should not be necessary to
reconstruct those amendments to implement publication policy.

**Useful acceptance cases for the first implementation:**

- A library and two consumers share a changeset; `--only` selects one
  consumer, and it still receives the candidate library.
- An amend changes the library's code without changing package coordinates;
  affected consumers cannot reuse results from the previous archive.
- A previously absent optional sourced file is added; its observation
  invalidates reuse even when no guest would otherwise run.
- A revision bump also edits a deferred build hook; the target is
  substantive and cannot use the revision-only acknowledgement policy.
- A guest disappears after two completed targets and during a third;
  completed results survive and retry has explicit artifact requirements.
- A one-port legacy contribution migrates without expanding the coverage
  claimed by its existing evidence to all newly derived sibling targets.

No implementation files or accepted design documents were changed for
this review. The only new repository artifact is this note. The evaluator
probe was local, synthetic, and did not fetch or build anything.
