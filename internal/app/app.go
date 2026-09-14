package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/credential/keychain"
	"github.com/herbygillot/dockhand/v2/internal/forge/github"
	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/prepare"
	"github.com/herbygillot/dockhand/v2/internal/proc"
	"github.com/herbygillot/dockhand/v2/internal/publish"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/herbygillot/dockhand/v2/internal/state/sqlite"
	"github.com/herbygillot/dockhand/v2/internal/upstream"
	"github.com/herbygillot/dockhand/v2/internal/verify"
	"github.com/herbygillot/dockhand/v2/internal/verify/tart"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
)

type Config struct {
	DBPath         string
	Repository     string
	GitExecutable  string
	TclExecutable  string
	MacPortsPrefix string
	Tart           tart.Config
	GitHub         github.Config
}

type Services struct {
	Workflow     *workflow.Engine
	Processes    *proc.Manager
	Preparation  *prepare.Service
	Discovery    *upstream.Service
	close        func() error
	verification *tart.Provider
	ports        *macports.Evaluator
}

func Build(ctx context.Context, config Config) (*Services, error) {
	if config.Repository == "" {
		config.Repository = "."
	}
	repo, err := git.Open(ctx, config.Repository, config.GitExecutable)
	if err != nil {
		return nil, err
	}
	store, err := sqlite.Open(ctx, config.DBPath, sqlite.Options{})
	if err != nil {
		return nil, err
	}
	repository, err := store.RegisterRepository(ctx, repo.CommonDir)
	if err != nil {
		store.Close()
		return nil, err
	}

	ports := &macports.Evaluator{Executable: config.TclExecutable, Prefix: config.MacPortsPrefix}
	githubClient := &github.Client{HTTP: http.DefaultClient, Config: config.GitHub}
	if config.GitHub.BaseURL == "" {
		githubClient.Credentials = github.SystemCredentials{Store: keychain.Store{}, Key: githubCredentialKey}
	}
	discovery := releaseDiscovery(ports, githubClient, http.DefaultClient)
	preparation := &prepare.Service{Repo: repo, Ports: ports, Upstream: discovery}
	if config.Tart.ArtifactDirectory == "" {
		config.Tart.ArtifactDirectory = filepath.Join(filepath.Dir(store.Path()), "artifacts", "tart")
	}
	if config.Tart.PortIndexExecutable == "" && config.MacPortsPrefix != "" {
		config.Tart.PortIndexExecutable = filepath.Join(config.MacPortsPrefix, "bin", "portindex")
	}
	provider := &tart.Provider{Config: config.Tart, State: store, Repository: repository.ID, Repo: repo}
	engine := &workflow.Engine{
		State:      store,
		Repository: repository.ID,
		Repo:       repo,
		Ports:      ports,
		Preparer:   preparation,
		Releases:   preparation,
		Planner:    &verify.Planner{Ports: ports},
		Provider:   provider,
		Publisher:  &publish.Service{Repo: repo, Forge: githubClient, LockDirectory: filepath.Join(filepath.Dir(store.Path()), "publication-locks")},
		Now:        time.Now,
	}
	return &Services{
		Workflow:     engine,
		Processes:    &proc.Manager{},
		Preparation:  preparation,
		Discovery:    discovery,
		close:        store.Close,
		verification: provider,
		ports:        ports,
	}, nil
}

func (s *Services) Close() error {
	if s != nil && s.close != nil {
		return s.close()
	}
	return nil
}

func Status(ctx context.Context, config Config) (workflow.Status, error) {
	return FilteredStatus(ctx, config, workflow.StatusFilter{})
}

func FilteredStatus(ctx context.Context, config Config, filter workflow.StatusFilter) (workflow.Status, error) {
	if err := filter.Validate(); err != nil {
		return workflow.Status{}, err
	}
	empty := func() (workflow.Status, error) {
		if filter.JobID != "" {
			return workflow.Status{}, fmt.Errorf("job %s: %w", filter.JobID, state.ErrNotFound)
		}
		result := workflow.EmptyStatus(time.Now())
		if filter != (workflow.StatusFilter{}) {
			result.Filter = &filter
		}
		return result, nil
	}
	root := config.Repository
	if root == "" {
		root = "."
	}
	repo, err := git.Open(ctx, root, config.GitExecutable)
	if err != nil {
		return workflow.Status{}, err
	}
	store, err := sqlite.Open(ctx, config.DBPath, sqlite.Options{ReadOnly: true})
	if errors.Is(err, state.ErrNoDatabase) {
		return empty()
	}
	if err != nil {
		return workflow.Status{}, err
	}
	defer store.Close()
	repository, err := store.FindRepository(ctx, repo.CommonDir)
	if errors.Is(err, state.ErrNotFound) {
		return empty()
	}
	if err != nil {
		return workflow.Status{}, err
	}
	engine := workflow.Engine{State: store, Repository: repository.ID}
	return engine.FilteredStatus(ctx, filter)
}
