# Verification coverage boundary

Accepted review 7's dependency finding: `verify.PlanDependents` imported the
native discovery implementation for its coverage input, pulling MacPorts
runtime, PortIndex staging, and HTTP dependencies into the provider contract.

A direct type move would not fix this: the old candidate contained a
`macports.Snapshot` and `portindex.Closure`. Added a consumer-owned `verify.Coverage`
contract with source-bound target/tool requirements, selection reasons, indexed
dependency estimates, and diagnostic gaps. The MacPorts discovery adapter now
validates native evaluations and projects those facts. Verification still checks
source/platform/target identity and requested roots, and freezes the same build
plans. Failed or unread Xcode requirements remain blocked questions.

Updated app wiring and the workflow discovery interface. Kept Git object-ID
validation in `git`, where that format belongs, rather than adding a new operation
to durable record types. No schema, serialized plan, scheduling, or publication
policy changed. Updated the component dependency rules, including the existing
PortIndex helpers and the verification staging boundary.

Validation: focused workflow regressions passed for durable cohort recovery,
claim replacement, independent attempts, and publication gating. The verify and
native dependent-discovery suites pass, including new checks for missing or
mismatched evaluation identity and true/false/invalid/unread Xcode requirements.
No v1 code or tests were copied.
