# 2026-09-22: no intake leaf

The [resolution design](../resolution-design.md)'s seventh step asked,
once the value had landed, whether the four bindings and `Resolve` lift
into `workflow/intake` as a leaf that takes the store, the repository,
the evaluator, and the forge as values and hands the engine a finished
request. The answer, read from the code after step 6, is no.

**The set.** The candidate is nine files: the preparation, verification,
publication, and correction bindings, adoption, the resolution, the
contribution lookup, the continuation check, and the onto target.
That is 2,223 of `workflow`'s 7,305 production lines, and 15 of its 35
test files reach it through the shared fixture, 4,338 lines of tests.

**It is not a leaf in either direction.** The set calls nine engine
helpers outside itself. Six are trivial. Two are middling: verification
target inference, and the publication destination, which reaches the
forge. One is large and writes: the continuation check refreshes the
pull request through the 166-line lifecycle method that updates state
and talks to the forge, and adoption writes the tracking record itself.
An intake leaf would therefore need the store, the repository, the
reader, the workspace registry, the publisher with its forge, the clock,
and the timeouts, which is the engine minus its four drive-only fields.
And the acceptance side calls back into the set: `Submit` takes the
request type and runs the branch adoption that lived in the adoption
file; submit, integrate, and reassociate use the correction's
transactional checks; lifecycle, control, and status use the
contribution lookup; reassociate and the release scope use the snapshot
binding. Making the engine import the leaf for its request type while
the leaf needs the engine's helpers is the mutual dependency the
[2026-09-16 review](../reviews/2026-09-16-bump-machinery-structure.md)
named as the reason not to split unless the shared pieces move cleanly.
They have grown since.

**The bindings read alike at their edges only.** All four open the same
way, a nil store, the required collaborators, a valid token, the
repository scope, and all four end in the same request. Between those
they are the four actions: a kind switch and a spec, a capture and a
snapshot binding, a plan through the publisher, a transaction with the
pull request and a message. The resolution made the lookup alike, which
was its job; it did not make the middles alike, and nothing will.

**The name is taken.** The [architecture](../architecture.md) uses
"intake" for the submit transaction, the durable acceptance under one
state transaction. A package by that name holding the bindings but not
`Submit` would contradict the vocabulary it defines.

**What a lift would not buy.** `app` talks only to the engine and the
CLI hands every bound request to `Submit`; neither gets simpler. The
gain would be a clearer split of the engine's fields and a smaller test
fixture per side, against a third package for the request type and the
shared checks, or duplication, and the pull request refresh handed back
through a callback.

**What was done instead.** `BranchInput`, its validation, and the
transactional branch adoption moved from the adoption file to the submit
side, where they are called: they are acceptance logic that sat beside
the adoption command because both adopt a branch, in different senses.
The design and the roadmap record the question as closed.
