// Package record defines Dockhand's durable workflow records and shared
// identities and value types.
//
// A Change tracks a contribution across immutable Revision values. A Job records
// an accepted request and its destination. An Attempt retains concrete build
// inputs and observed evidence; a Resource tracks environment ownership and
// cleanup independently of job completion. A PublicationAction records an
// individual effect, while a PullRequest retains the contribution's forge
// association. Provider records describe shared capacity and execution identity.
//
// Workflow and providers own progression, and state implementations own persistence
// and integrity checks. Fields, maps, and slices are ordinary Go values; callers
// preserve accepted inputs and copy mutable data when sharing ownership.
package record
