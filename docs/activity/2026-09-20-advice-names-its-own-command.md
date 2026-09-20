# Advice that names the contribution's own command

Five lines told a person what to run after work stopped, and each named a fixed verb. Four of them named the wrong one for most contributions.

A needs-attention job in the verification phase said "verify again once fixed". For a bump that is the weaker command: it re-runs the build and stops at a verified contribution, losing the pull request the job was going to open. That is the same defect fixed earlier today in the branch directly above it, which was left alone. A needs-attention or failed job in the preparation phase, and a retired contribution that stopped before building anything, all said to bump, so a stopped `refresh-checksums` was told to run a version bump, which acceptance refuses as a different preparation intent. A canceled job said "verify or publish again", losing intent the same way.

All five now read `retryCommand`, which already resolves to the contribution's own preparing action, to `publish` for a stopped publication, and to `verify` for a correction or for a branch dockhand never prepared. That last case is why this could not be a string substitution: an adopted branch has no bump to run, and the helper knows it.

The sentences keep their distinct meanings. "Fix it, then" stays where a person must act before anything will change, which is where an unsupported verdict lands, and it is absent from the advice for a failure beyond the change, where there is nothing to fix. A failed build still says to amend, because a contribution that did not build is the change's own business.

The verification line says one thing more. A provider rejection closes that request permanently, which sounds worse than it is: a submission's identity comes from its attempt, so a retry is a new attempt with a new identity and a fresh provider row, and the closed one is never consulted again. The line now says the command starts a new attempt, so the person is not left wondering whether they are resuming something broken.

A test walks the advice for a revision bump that stopped unsupported, a canceled bump, a failed checksum refresh, and a standalone verification, asserting each names its own command and that a revision bump is never told to verify.
