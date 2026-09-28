# 2026-09-28: private-helper ownership review

Reviewed `7be0dc2d992c0a84e9ac3f2fea1c27347db81bcf` as a complete source snapshot, focusing on private helpers that own another package's facts, duplicate another operation, or suggest a missing concept. Added the [review](../reviews/2026-09-28-private-helper-ownership.md) and [reproduction patch](../reviews/2026-09-28-private-helper-probes.patch).

The review distinguishes new, reproduced discrepancies from previously identified work still on the roadmap. It recommends focused source-comparison and MacPorts binary-archive packages, while putting most other operations in existing domain packages. It retains the prior decision against bringing macOS plist mechanics into command.

Validation used an isolated export of the commit. Seven probe functions all failed as described: option semantics, revision-only classification, three manifest-comparison cases, Tcl description encoding, and Cargo registry identity. The probe functions were not added to the working checkout's test suites. Existing archive, newport, and selection suites passed with probes excluded. The engine suite also passed (144 seconds), after granting local loopback access for its test HTTP servers.

The checkout advanced to `f43228acfdb60570d134757774632383931dd5e8` during the review. Its source delta was inspected: the earlier findings remain, and its new body-merge helpers supplied a tenth finding about returning section-level ownership and change information. Source citations and test results retain their explicit baseline.

Only review artifacts were added; no application code or roadmap changed.
