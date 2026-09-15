# Verification staging and workflow boundaries

Extracted `verify/staging` from Tart's input packing. It binds commit/tree identity, materializes the immutable source, stages and checks the exact PortIndex, and atomically installs an archive. Providers supply their own top-level payload files; Tart retains its guest script, manifest, admission timing, and resource lifecycle. Invalid payload names and canceled operations preserve an existing archive.

Split verification planning/reuse, due-attempt selection, and result recording into focused workflow files. The cycle still owns the transactions, claims, provider calls, and adoption checks. No new scheduler, state abstraction, dependency, or schema was introduced.

This work extracts existing v2 code and adds the staging boundary and its failure-path test. Existing Tart source/index tests still exercise the provider's composition. Normal and race tests passed for `verify/staging`, `verify/tart`, and `workflow`.
