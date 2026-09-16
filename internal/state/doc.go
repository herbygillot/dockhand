// Package state defines repository-scoped persistence and shared provider
// coordination for Dockhand workflows.
//
// Views provide a consistent read-only snapshot. Updates commit related record
// changes together, including claims that authorize later external work. Provider
// pools coordinate executions across repository registrations in the same store,
// and image caches retain disposable observations.
//
// Transaction callbacks execute once, use their supplied context, return write
// errors, and finish before the transaction deadline. They must not perform
// external work, nest store calls, or retain a reader or transaction afterward.
// A reader or transaction is used serially within its callback; store operations
// may run concurrently. Concrete backends own locking and schema management.
//
// Callers decide eligibility and policy. Stores preserve repository scope,
// relationships, immutable identities, and monotonic lifecycle checkpoints;
// workflow owns evidence reuse and publication authorization.
package state
