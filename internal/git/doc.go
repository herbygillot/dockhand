// Package git provides repository operations through a configurable Git executable.
//
// Repository supports immutable objects, source capture and materialization,
// checked edits, refs, and guarded local and remote branch updates. Explicit
// preconditions and operation locks protect mutations shared by drivers. Callers
// own the lifetime of materialized snapshots and the workflow meaning of branches
// and commits; this package supplies Git facts and mechanisms.
package git
