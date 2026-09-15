package verify

import (
	"context"
	"errors"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
)

var ErrNotImplemented = errors.New("verify: verification planning is not implemented")

type Capabilities struct {
	Name string
	// Empty Platforms defers platform validation to Submit.
	Platforms []record.Platform
	Isolated  bool
	Capacity  int
}

type SubmissionState string

const (
	Admitted            SubmissionState = "admitted"
	AtCapacity          SubmissionState = "at-capacity"
	Unsupported         SubmissionState = "unsupported"
	SubmissionUncertain SubmissionState = "uncertain"
)

type Request struct {
	ID        record.RequestID
	AttemptID record.AttemptID
	Spec      record.BuildSpec
}

type Submission struct {
	State     SubmissionState
	Run       record.ProviderRun
	Resources []record.ResourceHandle
	Detail    string
}

type Observation struct {
	Workflow     *record.WorkflowEvidence `json:",omitempty"`
	TestOmission string                   `json:",omitempty"`
	Run          record.ProviderRun
	State        record.AttemptState
	Environment  *record.EnvironmentEvidence `json:",omitempty"`
	Steps        []record.StepResult
	Artifacts    []record.Artifact
	Logs         []record.Artifact
	Detail       string
	Verdict      record.Verdict
	Failure      *record.Failure
	ObservedAt   time.Time
}

type ReconciliationState string

const (
	RunFound      ReconciliationState = "found"
	RequestClosed ReconciliationState = "closed"
	RunUnknown    ReconciliationState = "unknown"
)

// ReconcileOptions distinguishes continuing admission from stopping a request.
// CancelRequested forbids starting new external work; an existing run may
// still be returned for cancellation or observation.
type ReconcileOptions struct {
	CancelRequested bool
}

// A closed request with an Unsupported submission records a definitive rejection,
// rather than permission to retry admission with a new request identity.
type Reconciliation struct {
	State      ReconciliationState
	Submission Submission
}

type ReleaseResult struct {
	Confirmed bool
	Detail    string
}

type Provider interface {
	Capabilities(context.Context) (Capabilities, error)
	// Submit enforces capacity and is idempotent by request ID across processes.
	Submit(context.Context, Request) (Submission, error)
	// Reconcile returns an existing run, durably closes a request to further submission, or reports uncertainty.
	// A closed request must reject every later Submit, including calls from stale drivers.
	Reconcile(context.Context, record.RequestID, ReconcileOptions) (Reconciliation, error)
	Observe(context.Context, record.ProviderRun) (Observation, error)
	Cancel(context.Context, record.ProviderRun) error
	Release(context.Context, record.ResourceHandle) (ReleaseResult, error)
}

type LogChunk struct {
	Data     []byte
	Next     int64
	Complete bool
}

type LogReader interface {
	ReadLog(context.Context, record.ProviderRun, int64, int) (LogChunk, error)
}
