package app

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/v2/internal/forge/github"
	"github.com/herbygillot/dockhand/v2/internal/prepare"
	"github.com/herbygillot/dockhand/v2/internal/proc"
	"github.com/herbygillot/dockhand/v2/internal/upstream"
	"github.com/herbygillot/dockhand/v2/internal/verify/tart"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
)

var ErrNotImplemented = errors.New("app: dependency construction is not implemented")

type Config struct {
	Repository     string
	GitExecutable  string
	TclExecutable  string
	MacPortsPrefix string
	Tart           tart.Config
	GitHub         github.Config
}

type Services struct {
	Workflow    *workflow.Engine
	Processes   *proc.Manager
	Preparation *prepare.Service
	Discovery   *upstream.Service
}

func Build(ctx context.Context, config Config) (*Services, error) {
	return nil, ErrNotImplemented
}

func Setup(ctx context.Context, config Config) error {
	return ErrNotImplemented
}
