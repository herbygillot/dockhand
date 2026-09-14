# PortIndex dependency discovery

Extended `macports/portindex` from index preparation into the read boundary needed by dependent verification. The package opens the generated `PortIndex`, optionally uses `PortIndex.quick` for case-insensitive lookup, repairs a stale accelerator from the primary index, and enumerates the primary index sequentially for whole-tree questions.

The reader honors the Tcl UTF-16 code-unit length used by MacPorts record framing, including non-ASCII and supplementary characters. It bounds declared record sizes, rejects duplicate or malformed names and records, and treats the quick file only as an accelerator rather than source truth.

Reverse discovery indexes direct `depends_lib`, `depends_build`, and `depends_run` relationships. Each result retains the declaring dependency fields and the dependent's own relevant requirements, so later coverage policy can distinguish build-only edges without rescanning. Forward closure follows every MacPorts dependency phase transitively and reports missing names and fields that could not be parsed instead of presenting a silently incomplete answer.

The package does not decide which dependents require revision edits or verification. It also does not call workflow or Tart. The next integration step must run these queries against the exact prepared source outside a SQLite transaction, evaluate selected dependents for their build configuration, and persist the resulting verification plan.

All implementation and tests in this change were authored for v2 after reviewing the PortIndex format and v1 behavior. No v1 comments or tests were copied.

Focused tests cover stale quick-index recovery, case-insensitive lookup, Tcl string-length framing, dependency-token forms, build-only classification, reverse edges, transitive closure, and missing dependencies. A smoke run against the local 41,684-entry MacPorts tree completed a full reverse index and closure without unread fields.
