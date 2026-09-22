# 2026-09-22: one owner for a contribution's revision, and an editing session

Two structural items the [follow-up review](../reviews/2026-09-22-architecture-follow-up.md)
put ahead of any further extraction, done the same evening in two
commits. Neither changes behavior; each gives a thing that had two
owners one.

## The contribution, in `workflow`

An update prepared onto a contribution and a person's correction of it
replace the same revision, and until this evening each computed for
itself what the replacement must keep: the onto target read the change,
its current revision, its pull request, and the remote head in one
transaction, and `BindCorrection` read the same four in another, in the
same order, with the same checks, and then each made its own correction
spec, its own publication destination, and its own keep-body guard.

`contribution` in [contribution.go](../../internal/workflow/contribution.go)
is the one owner now: the change, its current revision, its attached
pull request, and the remote head a replacement expects, bound by
`bindContribution` with the preconditions integration rechecks. It
answers what a revision must keep. `correction` is the replacement spec
with the revision's scope on it, the head the branch must still have,
and the candidate when one was captured. `destination` is the pull
request's, whoever owns its head, and the checkout's remotes otherwise.
`allowsPublication` is the keep-body guard. `selection` is the one
target with the person's variants laid over it. The message and scope
rules the previous change shared, `revisedMessage` and `revisedScope`,
live in the same file. The preparation binding, the correction binding,
and the resolution's onto kind all read the one type; the onto file is
gone, and `BindCorrection` lost its transaction.

## The editing session, in `portedit`

`sourceInput` held two lifetimes in one struct: the workspace, the
native interpreter, and the candidate overlays, which live as long as
the preparation, and the baseline, the snapshot, the targets, the
contents, and the release inputs, which a derived baseline replaces.
The derived baseline for a dependency regeneration was a shallow copy
of the whole struct with five fields reassigned, and another member's
view for the shared-archive check was a copy with one; both shared the
interpreter and the overlay holder by the accident of pointer copy.

Two types now. `session` owns the workspace and its tree, the
interpreter opened on first use, the contents as loaded, and the
overlays with their by-contents cache; it makes overlays and it closes
everything, once. `sourceInput` is one baseline of a session: it embeds
the session by pointer and holds the snapshot, the targets, the
contents, the family, the release scope and version input, and its
observation session. `derive` makes the stripped baseline explicitly,
naming what carries over and what is replaced, and `forMember` makes
another member's view. Only the input that opened the session closes
it. The workspace design's third step said the derived baseline becomes
its own overlay; it does, through the session's cache, and the shallow
copy is gone.

## Size

| | before | after |
|---|---|---|
| Owners of a revision's preconditions in `workflow` | 2 | 1 |
| Copies of `sourceInput` by value in `portedit` | 2 | 0 |
| Lines in `workflow`, net | | +7, a 181-line owner for 202 lines over four files |
| Lines in `portedit`, net | | +28, the two types and their comments |

The messages a person sees are unchanged. The onto test asserts the
correction spec by value as before, and the input's path test now says
that another member's view shares the session.
