package workflow

import (
	"errors"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/model"
	"github.com/herbygillot/dockhand/v2/internal/prepare"
	"github.com/herbygillot/dockhand/v2/internal/publish"
	"github.com/herbygillot/dockhand/v2/internal/verify"
)

var ErrNotImplemented = errors.New("workflow: job execution is not implemented")

type Engine struct {
	Ledger    *ledger.Store
	Preparer  *prepare.Service
	Planner   *verify.Planner
	Provider  verify.Provider
	Publisher *publish.Service
	Now       func() time.Time
}

type Scope struct {
	All  bool
	Jobs []model.JobID
}
