// Package git provides repository operations through a configurable Git executable.
//
// Repository supports immutable objects, materialization, checked edits, refs,
// history replay, and guarded local and remote branch updates. Explicit
// preconditions and a branch lock protect what concurrent dockhand processes
// both change. Callers own the lifetime of materialized snapshots and the
// meaning of branches and commits; this package supplies Git facts and
// mechanisms.
package git
