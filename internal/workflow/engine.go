package workflow

import (
	"errors"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/prepare"
	"github.com/herbygillot/dockhand/v2/internal/publish"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/verify"
)

var (
	ErrNotImplemented    = errors.New("workflow: job execution is not implemented")
	ErrNoLedger          = errors.New("workflow: ledger is required")
	ErrInvalidRequest    = errors.New("workflow: invalid request")
	ErrUnsupportedAction = errors.New("workflow: action intake is not implemented")
	ErrRequestConflict   = errors.New("workflow: request ID already has different intent")
	ErrStaleRevision     = errors.New("workflow: selected revision is stale")
	ErrNotFound          = errors.New("workflow: record not found")
	ErrClaimLost         = errors.New("workflow: action claim expired or was replaced")
	ErrInvalidScope      = errors.New("workflow: select all jobs or explicit job IDs")
)

type Engine struct {
	Ledger        *ledger.Store
	Preparer      *prepare.Service
	Planner       *verify.Planner
	Provider      verify.Provider
	Publisher     *publish.Service
	Now           func() time.Time
	Owner         record.ProcessID
	LeaseDuration time.Duration
	CallTimeout   time.Duration
	RetryDelay    time.Duration
}

type Scope struct {
	All  bool
	Jobs []record.JobID
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}
