// Package state defines repository-scoped persistence for Dockhand workflows.
// Views provide a consistent read-only snapshot. Updates commit related record
// changes together, including the claims used to authorize later external work.
// Callbacks execute once and must use their supplied context, return write
// errors, and finish before the transaction deadline. They must not perform
// external work, nest store calls, or retain a reader or transaction afterward.
// A reader or transaction is used serially within its callback. Store operations
// may run concurrently. Concrete backends own locking and schema management.
package state
