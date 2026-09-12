// Package record defines Dockhand's durable workflow records and their shared
// identities and value types.
//
// A [Change] tracks a contribution across immutable [Revision] values. A [Job] records one
// accepted request and its destination. An [Attempt] retains concrete build inputs
// and observed evidence; a [Resource] tracks environment ownership and cleanup
// independently of job completion. A [PublicationAction] tracks an individual effect,
// while a [PullRequest] retains the contribution's association with a forge.
//
// The workflow package owns state transitions, and the ledger package owns
// encoding and persistence. This package supplies data definitions for those
// boundaries, including operations whose execution is still being implemented.
// Fields, maps, and slices are ordinary Go values; callers are responsible for
// preserving accepted inputs and copying mutable data when sharing ownership.
package record
