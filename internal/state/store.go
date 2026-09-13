package state

import (
	"context"
	"errors"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
)

var (
	ErrNotFound    = errors.New("state: record not found")
	ErrNoDatabase  = errors.New("state: database does not exist")
	ErrInvalid     = errors.New("state: invalid record or operation")
	ErrConflict    = errors.New("state: identity or precondition conflict")
	ErrUnavailable = errors.New("state: storage unavailable")
	ErrSchema      = errors.New("state: unsupported database schema")
	ErrUncertain   = errors.New("state: commit outcome uncertain")
	ErrReadOnly    = errors.New("state: read-only store")
)

type Store interface {
	FindRepository(context.Context, string) (record.Repository, error)
	RegisterRepository(context.Context, string) (record.Repository, error)
	View(context.Context, record.RepositoryID, func(context.Context, Reader) error) error
	Update(context.Context, record.RepositoryID, func(context.Context, Tx) error) error
}

// Query limits record enumeration. Jobs narrows the query within its repository;
// a nil Jobs slice includes that repository, while an empty non-nil slice is empty.
// DueBefore selects scheduled work. After is an exclusive ID cursor.
type Query struct {
	Jobs      []record.JobID
	DueBefore *time.Time
	After     string
	Limit     int
	Pending   bool
}

// VerificationQuery bounds candidate lookup to a repository, target, and optional tree.
// Tree omission is for a recent-result diagnostic, not proof of applicability.
// Limit must be between 1 and 32. Results contain original terminal attempts with evidence, newest first.
type VerificationQuery struct {
	Target record.Target
	Tree   record.ObjectID
	Limit  int
}

type Reader interface {
	VerificationCandidates(context.Context, VerificationQuery) ([]record.Attempt, error)
	Change(context.Context, record.ChangeID) (record.Change, error)
	OpenChangeByBranch(context.Context, string) (record.Change, error)
	Revision(context.Context, record.RevisionID) (record.Revision, error)
	Request(context.Context, record.RequestID) (record.AcceptedRequest, error)
	Job(context.Context, record.JobID) (record.Job, error)
	JobForRequest(context.Context, record.RequestID) (record.Job, error)
	Attempt(context.Context, record.AttemptID) (record.Attempt, error)
	Plan(context.Context, record.JobID) (record.VerificationPlan, error)
	Submission(context.Context, record.RequestID) (record.Submission, error)
	Resource(context.Context, record.ResourceID) (record.Resource, error)
	Jobs(context.Context, Query) ([]record.Job, error)
	Changes(context.Context, Query) ([]record.Change, error)
	Revisions(context.Context, Query) ([]record.Revision, error)
	AttemptsForJob(context.Context, record.JobID) ([]record.Attempt, error)
	SubmissionsForAttempt(context.Context, record.AttemptID) ([]record.Submission, error)
	ResourcesForAttempt(context.Context, record.AttemptID) ([]record.Resource, error)
	Resources(context.Context, Query) ([]record.Resource, error)
	Control(context.Context, record.RequestID) (record.ControlRequest, error)
	Controls(context.Context, Query) ([]record.ControlRequest, error)
}

type Writer interface {
	PutChange(context.Context, record.Change) error
	PutRevision(context.Context, record.Revision) error
	PutRequest(context.Context, record.AcceptedRequest) error
	PutJob(context.Context, record.Job) error
	PutPlan(context.Context, record.VerificationPlan) error
	PutAttempt(context.Context, record.Attempt) error
	PutSubmission(context.Context, record.Submission) error
	PutResource(context.Context, record.Resource) error
	PutControl(context.Context, record.ControlRequest) error
	ApplyControl(context.Context, record.RequestID, record.JobID, time.Time) error
}

type Tx interface {
	Reader
	Writer
}
