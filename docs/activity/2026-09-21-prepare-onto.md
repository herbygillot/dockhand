# Preparing onto a contribution, and the rest of adopt

The second half of the adopt design, built the same evening.

**Prepare onto the contribution.** Every preparation started from fetched master, and a port with an open contribution of the same action continued it from the master commit it was prepared from, which is right for retrying a bump dockhand made and useless for the two other cases: a port that exists only on its branch, the dockhand port, whose checksums had to be computed by hand twice today; and a different edit on an existing contribution, `checksums` on a bump whose tarball was re-rolled. The prior-job lookup now reports the open contribution as well as the job, and a contribution whose current revision is not the last preparation's result, adopted or amended since, is prepared onto that revision's tree: the same editor runs on the branch's Portfile, the result becomes a correction whose commit carries the prepared tree and keeps the contribution's message unless `--subject` is given, and the amend job verifies and publishes it. `--dry-run` finds the contribution through a read-only look at the state database and previews the diff against the branch. A revision bump onto a contribution needs no subject, since the contribution's message is its reason, and the editor is handed that message's subject so its own rule holds.

**`--adopt` on the preparations.** `bump`, `bump-revision`, and `checksums` take `--adopt <branch>` and track the branch first, then prepare onto it; with `--dry-run` the adoption is a dry run too, which reads the state database to check the branch is untracked and records nothing.

**`adopt --squash`.** A branch of several commits above master is folded into one on its merge base, carrying the branch's tree and the oldest commit's message, with the originals kept under `refs/dockhand/adopted/<branch>`; a dry run computes the fold and moves nothing. Two git helpers joined the two from the morning: the merge base and the oldest commit above it.

**One rule the tests forced.** Adopting a second branch for a port that already has an open contribution was accepted, which would have made the port's contribution ambiguous for every verb; adopt now refuses it and names the branch to amend or abandon.

**Left as the design item.** Multi-target contributions, one commit per port directory in dependency order, with adopt refusing several directories until then.
