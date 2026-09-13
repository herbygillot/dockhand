package verify

import (
	"context"
	"errors"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
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
	Run        record.ProviderRun
	State      record.AttemptState
	Steps      []record.StepResult
	Artifacts  []record.Artifact
	Logs       []record.Artifact
	Detail     string
	Verdict    record.Verdict
	Failure    *record.Failure
	ObservedAt time.Time
}

type ReconciliationState string

const (
	RunFound      ReconciliationState = "found"
	RequestClosed ReconciliationState = "closed"
	RunUnknown    ReconciliationState = "unknown"
)

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
	// Reconcile returns an existing run, durably closes an unadmitted request, or reports uncertainty.
	// A closed request must reject every later Submit, including calls from stale drivers.
	Reconcile(context.Context, record.RequestID) (Reconciliation, error)
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
