# Architecture and data-flow review

Reviewed the v3 architecture at `e8e62eb415096f13ca24fbf60e4d110554410ede`,
including the preceding Golden Gate image change. Added the
[review](../reviews/2026-09-27-architecture-and-data-flow.md) and a
[regression-probe patch](../reviews/2026-09-27-architecture-regression-probes.patch).

The review maps active and legacy packages, identifies lossy planning,
authoring, execution, and history boundaries, and recommends staged extraction
of check policy, execution contracts, and history operations. Seven isolated
probes reproduce test-policy/reporting errors, incomplete environment planning,
baseline binding errors, and a rebase restore that leaves the wrong base.

The existing model, store, coordination, engine, command, provider, and Tart
suites passed in an exported copy of the reviewed commit. The probes assert the
intended behavior and fail as documented. They remain a patch artifact rather
than changes to the production test suite. No live provider build or publication
was performed, and production files were not modified. Concurrent working-tree
edits were left untouched. No commit or push was made.
