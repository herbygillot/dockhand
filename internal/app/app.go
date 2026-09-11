package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/forge/github"
	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/prepare"
	"github.com/herbygillot/dockhand/v2/internal/proc"
	"github.com/herbygillot/dockhand/v2/internal/publish"
	"github.com/herbygillot/dockhand/v2/internal/upstream"
	"github.com/herbygillot/dockhand/v2/internal/verify"
	"github.com/herbygillot/dockhand/v2/internal/verify/tart"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
)

var ErrNotImplemented = errors.New("app: setup is not implemented")

type Config struct {
	Lockfile       string
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
	if config.Repository == "" {
		config.Repository = "."
	}
	repo, err := git.Open(ctx, config.Repository, config.GitExecutable)
	if err != nil {
		return nil, err
	}
	store, err := ledger.New(repo, ledger.Options{Lockfile: config.Lockfile})
	if err != nil {
		return nil, err
	}
	ports := &macports.Evaluator{Executable: config.TclExecutable, Prefix: config.MacPortsPrefix}
	forge := &github.Client{HTTP: http.DefaultClient, Config: config.GitHub}
	discovery := &upstream.Service{Ports: ports, Releases: forge}
	preparation := &prepare.Service{Ports: ports, Upstream: discovery}
	engine := &workflow.Engine{
		Ledger:    store,
		Preparer:  preparation,
		Planner:   &verify.Planner{Ports: ports},
		Provider:  &tart.Provider{Config: config.Tart},
		Publisher: &publish.Service{Repo: repo, Forge: forge},
		Now:       time.Now,
	}
	return &Services{
		Workflow:    engine,
		Processes:   &proc.Manager{CommonDir: repo.CommonDir},
		Preparation: preparation,
		Discovery:   discovery,
	}, nil
}

func Setup(ctx context.Context, config Config) error {
	return ErrNotImplemented
}
