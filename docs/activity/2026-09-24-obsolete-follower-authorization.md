# 2026-09-24: a named subport's bump moves its obsolete follower

Step 1 of the roadmap's Next. `bump kubectl-1.37` (2026-09-23) previewed
the right diff, moving the obsolete `kubectl` stub's `version 1.37.0`
along with the subport that replaces it, and then the job stopped with
"workflow: unapproved shared-release scope" ([direction
record](../reviews/2026-09-23-contracts-direction.md), "Found along the
way").

Two rules disagreed. The editor's (`fidelity.ReleaseScope`) lets an
obsolete follower, a port replaced by the target that carries its version
and builds nothing, move without shared-release authorization, and
records a scope holding it (`portedit/version.go`). The workflow's
(`workflow/preparation_run.go`) refused every recorded scope the job had
not authorized, and binding authorizes only a stub's bump and a main
port's. The preview runs the editor and never the workflow's check, so it
could not see the refusal coming.

Now there is one rule, on the record:

- `record.ReleaseMember.Follower` says why a member is in the scope. It
  is `omitempty`, so records without followers encode as before, and an
  old record decodes as needing authorization, which is what it was
  judged by. No migration.
- `ReleaseMember.NeedsAuthorization(initiating)` is true for every member
  but the initiating target, except a follower, and
  `ReleaseScope.NeedsSharedRelease` asks it of every affected member.
- The editor sets `Follower` and refuses through `NeedsAuthorization`;
  the workflow refuses a scope only when `NeedsSharedRelease` holds and
  the job was not authorized. The preview reaches the same predicate
  through the editor, whose authorization for a bump (the flag, a stub,
  or a main port) is the one binding records for the job.

Tests: the editor's follower test asserts the flag and that the scope
needs no authorization; a record test covers the predicate and the
encoding; and `TestNamedSubportBumpMovesItsObsoleteFollowerWithoutAuthorization`
runs a named subport's bump through the workflow cycle with a follower,
which now prepares (and failed before the change), and with an
unauthorized sibling, which is still refused.
