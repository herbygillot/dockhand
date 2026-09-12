package record

// ChangeID identifies a contribution across branch rewrites and jobs.
type ChangeID string

// RevisionID identifies one immutable source snapshot of a change.
type RevisionID string

// JobID identifies one accepted workflow request's execution record.
type JobID string

// RequestID identifies an idempotent workflow request or provider submission.
type RequestID string

// AttemptID identifies one execution of a concrete verification specification.
type AttemptID string

// ResourceID identifies a resource ownership and cleanup record in the ledger.
type ResourceID string

// PublicationID identifies one action to create or update a pull request.
type PublicationID string

// PullRequestID identifies a tracked pull request within Dockhand's ledger.
type PullRequestID string

// TargetID identifies a target and configuration within a verification plan.
type TargetID string

// ProcessID identifies a driver claim owner independently of its operating-system PID.
type ProcessID string

// ObjectID is a full Git object identifier for a commit, tree, or blob.
type ObjectID string
