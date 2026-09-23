package tart

import (
	"context"
	"errors"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"net/http"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

var errClosed = errors.New("tart: submission is permanently closed")

var errCapacity = errors.New("tart: pool is at capacity")

type Provider struct {
	Config Config
	// IndexCache is the PortIndex cache root shared with discovery; it is
	// disposable and therefore not part of the frozen configuration.
	IndexCache string
	State      state.ProviderStore
	Repository record.RepositoryID
	Repo       *git.Repository
	// Workspaces shares the prepared tree with dependent discovery; nil
	// materializes one for staging alone.
	Workspaces *workspace.Registry
	HTTP       *http.Client
	backend    machine
	images     imageCache
}

type machine interface {
	Version(context.Context) (string, error)
	Environment(context.Context) (Environment, error)
	InspectCapabilities(context.Context, string, string) (capabilityInspection, error)
	Running(context.Context) ([]string, error)
	Clone(context.Context, string, string) error
	Start(context.Context, string, string) error
	Ready(context.Context, string) error
	Stage(context.Context, string, string) error
	Launch(context.Context, string) error
	Inspect(context.Context, string) (guestResult, error)
	Logs(context.Context, string, string) error
	Stop(context.Context, string) error
	Delete(context.Context, string) error
}
