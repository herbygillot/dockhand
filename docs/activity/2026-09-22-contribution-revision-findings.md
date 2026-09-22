# 2026-09-22: what an update onto a contribution keeps

The [follow-up review](../reviews/2026-09-22-architecture-follow-up.md)
of the day's work found three defects at one boundary, the point where
the unified preparation pipeline replaces a contribution's revision, and
asked for the resolution's contract to be tightened before more callers
depend on it. All four were verified against the code, two claims under
the first finding were answered in the review itself, and the four were
done in two commits.

## The three defects, one cure

The onto target, what an update prepared onto a contribution binds to,
now carries what the contribution already settled, and the durable
preparation keeps it.

- **Release scope.** The prior revision's scope travels on the
  correction spec, and the candidate's scope is settled before the branch
  moves: an edit's own scope must keep the contribution's membership, and
  an edit with none, a checksum refresh or a revision bump, carries the
  contribution's, rebound onto the candidate as a correction rebinds it.
  Before, the store refused the revision after the branch had already
  been replaced, and the records and the branch disagreed from then on;
  the review's consequence, a retry meeting the same candidate, was the
  milder reading, and the response in the review says why. The store now
  lets a scoped revision follow an unscoped one, an adopted branch's, and
  fixes the membership from then on; a scoped revision can still be
  followed only by the same membership. The rule the review asked to be
  defined already existed in the store; what was missing was its
  enforcement at binding.
- **Publication destination.** The contribution's open pull request is
  retained on the onto target, and the binding resolves the destination
  through the helper the correction path already used, so a maintainer
  updating an adopted pull request from someone else's fork publishes to
  that fork. Before, the checkout's remotes chose, and publication refused
  the result much later with "existing PR destination cannot change".
- **Commit message.** The candidate's message is the contribution's own,
  with the update's subject laid over the first line under the name the
  message carries and the update's references added where it does not
  cite them, through one helper the correction path and the preparation
  share. Before, a generated message replaced the body and the trailers,
  and the adopt test asserted only the subject.

One workflow test covers all three through the durable job, with a
scoped revision and an attached pull request on someone else's fork, and
internal tests cover the two rules on their own; the store's membership
rule has its own test.

## The resolution, tightened

- `Lookup`, `Require`, and `Offline` had no production callers after
  step 5 made a verification's resolution implied by its action, and the
  nil-store path bypassed `Require`. All three are gone.
- `Continue` meant two things: a preparation continuing a prior job's
  recorded input, and a verification's contribution as recorded. The
  second is its own kind, `Tracked`, with its own guarantee: the change
  and its current revision, and nothing decided about master or a prior
  job. The verification binding takes a `Tracked` resolution and refuses
  any other kind.
- Adoption made `Resolve` a write in one mode. It is the caller's write
  now, made first through `app.Adopt`, in a dry run for a preview, and
  `Engine.ResolveAdopted` reads its result into the Adopt kind. `Resolve`
  reads and never writes, with or without a store.

## Left as the review left it

The contribution owner inside `workflow`, which the review puts before
any extraction and which the [no-intake-leaf](2026-09-22-no-intake-leaf.md)
note agrees with; the `portedit` session and baseline split, which the
review sharpens into two types and the roadmap already carried as the
derived-baseline item; and the cycle's sequential operations and the
execution updater's reflection, carried from the earlier review. The
roadmap holds all four.
