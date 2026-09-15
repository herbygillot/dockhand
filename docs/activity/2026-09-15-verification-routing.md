# Verification routing and workflow evidence

Added provider-name routing for driver operations, cleanup, and artifact pruning, with one capability observation per provider per cycle. The single-provider field remains a fallback for embedders and existing fixtures. Publication and verification share a named remote-branch lock around the existing conditional push operation.

Added workflow-policy evidence with pinned commit, branch, run attempt, and matrix jobs. Persisted branch selection in existing SQLite JSON options. Publication accepts applicable workflow evidence without marking unobserved port phases successful; reuse requires the exact commit and branch. Tart refuses the workflow-controlled test policy.

Authored these interfaces and adaptations plus tests for mixed-provider routing, workflow evidence reuse, and continuation through publication. Full tests and vet passed; focused race tests cover routing and workflow publication. No schema migration or live remote operation was required.
