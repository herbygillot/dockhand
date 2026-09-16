# Scoped evaluation and archive observations

Added an explicit `macports.Observer` contract implemented by `macports/eval`. Every observation starts a fresh interpreter, records the native runtime separately, and applies any modeled platform only to that session. Normal `Evaluate` remains native. Optional execution traces retain option arguments and source frames; syntax must identify a unique declaration before edits can be made.

Native MacPorts fetch planning resolves mirror/site tags and assembles URLs without executing fetch hooks or downloading archives. `macports/distfiles` associates these observations with exact literal checksum spans, including nested declarations and append/prepend operations. Identical checksum values at different source locations remain distinct. Unowned, calculated, or overwritten values fail the association check.

Validation: native integration fixtures check profile isolation, scoped subport declarations, equal digests in architecture branches, appended auxiliary archives, and uninstrumented metadata equivalence. Existing evaluator and Portfile tests pass. No new Go dependencies or v1 comments/tests were copied.
