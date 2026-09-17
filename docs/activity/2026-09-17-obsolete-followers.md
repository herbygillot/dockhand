# Obsolete followers move with the subport that replaces them

Noticed by the user on 2026-09-17 after PR 34728: the terraform Portfile's main `terraform` port is an obsolete port, `replaced_by ${latestVersion}` (terraform-1.16) with its own literal `version 1.16.0`, and the bump of terraform-1.16 to 1.16.3 left that literal behind. Assess had seen both inputs; the edit only touched the target's own version input, and the shared-release scope, which keys on shared distfiles, could not include a port with no distfiles.

## The rule

A sibling in the same Portfile is an obsolete follower of the target when it is `replaced_by` the target, builds nothing, and carries the target's version before and after the edit. After the target's single version edit is chosen, `followObsolete` looks for exactly one literal in the follower's selection that spells the old version, replaces it with the new one, re-evaluates, and keeps the edit only if the follower's version moved, its revision stayed 0, and no other port changed at all. Ambiguity, a nonzero revision, a different version, or any collateral change leaves the follower for a person, silently, since it was never promised.

The evaluator now exports `replaced_by` and treats any port with it as metadata-only: an obsolete port never builds. `fidelity.ReleaseScope` admits a follower without `--shared-release` authorization, and `ScopedVersion` applies the per-member check to the target and its followers in every mode, so the follower is a recorded, metadata-only member of the release scope and appears in previews and status as "affected: terraform 1.16.0 -> 1.16.3 (metadata only)". Build targets exclude it. Independent series and unrelated siblings remain protected exactly as before.

The test uses a terraform-shaped fixture: a versioned subport, an older series, and an obsolete main port that follows the newer one; a main port at a different version is left alone.
