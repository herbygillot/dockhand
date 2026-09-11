package verify

import (
	"context"
	"errors"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/model"
)

var ErrNotImplemented = errors.New("verify: verification planning is not implemented")

type Capabilities struct {
	Name      string
	Platforms []model.Platform
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
	ID        model.RequestID
	AttemptID model.AttemptID
	Spec      model.BuildSpec
}

type Submission struct {
	State     SubmissionState
	Run       model.ProviderRun
	Resources []model.ResourceHandle
	Detail    string
}

type Observation struct {
	Run        model.ProviderRun
	State      model.AttemptState
	Steps      []model.StepResult
	Artifacts  []model.Artifact
	Logs       []model.Artifact
	Detail     string
	ObservedAt time.Time
}

type LookupState string

const (
	RunFound   LookupState = "found"
	RunAbsent  LookupState = "absent"
	RunUnknown LookupState = "unknown"
)

type Lookup struct {
	State      LookupState
	Submission Submission
}

type ReleaseResult struct {
	Confirmed bool
	Detail    string
}

type Provider interface {
	Capabilities(context.Context) (Capabilities, error)
	Submit(context.Context, Request) (Submission, error)
	Lookup(context.Context, model.RequestID) (Lookup, error)
	Observe(context.Context, model.ProviderRun) (Observation, error)
	Cancel(context.Context, model.ProviderRun) error
	Release(context.Context, model.ResourceHandle) (ReleaseResult, error)
}
