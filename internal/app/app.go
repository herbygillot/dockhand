package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	forgegithub "github.com/herbygillot/dockhand/internal/forge/github"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/proc"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/verify"
	githubverify "github.com/herbygillot/dockhand/internal/verify/github"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/herbygillot/dockhand/internal/workflow/preparation"
)

type Config struct {
	VerificationProvider    string
	VerificationDestination publish.Options
	DependencyTools         dependency.Tools
	DBPath                  string
	Repository              string
	GitExecutable           string
	TclExecutable           string
	MacPortsPrefix          string
	Tart                    tart.Config
	GitHub                  github.Config
}

type Services struct {
	Workflow          *workflow.Engine
	Processes         *proc.Manager
	Preparation       *preparation.Service
	close             func() error
	tartVerification  tartBuildConfigurator
	ports             *macports.Evaluator
	providerName      string
	githubClient      *github.Client
	githubDestination publish.Options
}

func Build(ctx context.Context, config Config) (*Services, error) {
	if config.VerificationProvider != "" && config.VerificationProvider != "auto" && config.VerificationProvider != "tart" && config.VerificationProvider != "github" {
		return nil, fmt.Errorf("unknown verification provider %q", config.VerificationProvider)
	}
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
	githubClient := newGitHubClient(config.GitHub)
	discovery := releaseDiscovery(ports, githubClient, http.DefaultClient)
	preparation := &preparation.Service{Repo: repo, Ports: ports, Upstream: discovery, DependencyTools: config.DependencyTools}
	if config.Tart.ArtifactDirectory == "" {
		config.Tart.ArtifactDirectory = filepath.Join(filepath.Dir(store.Path()), "artifacts", "tart")
	}
	if config.Tart.PortIndexExecutable == "" && config.MacPortsPrefix != "" {
		config.Tart.PortIndexExecutable = filepath.Join(config.MacPortsPrefix, "bin", "portindex")
	}
	provider := &tart.Provider{Config: config.Tart, State: store, Repository: repository.ID, Repo: repo}
	githubProvider := &githubverify.Provider{State: store, Repository: repository.ID, Repo: repo, Directory: filepath.Join(filepath.Dir(store.Path()), "github-verification"), Client: githubClient}

	engine := &workflow.Engine{
		State:      store,
		Repository: repository.ID,
		Repo:       repo,
		Ports:      ports,
		Preparer:   preparation,
		Releases:   preparation,
		Provider:   provider,
		Providers:  map[string]verify.Provider{"tart": provider, "github": githubProvider},
		Publisher:  &publish.Service{Repo: repo, Forge: &forgegithub.Client{Client: githubClient}, LockDirectory: filepath.Join(filepath.Dir(store.Path()), "publication-locks")},
		Now:        time.Now,
	}
	return &Services{
		Workflow:          engine,
		Processes:         &proc.Manager{},
		Preparation:       preparation,
		close:             store.Close,
		tartVerification:  provider,
		ports:             ports,
		providerName:      config.VerificationProvider,
		githubClient:      githubClient,
		githubDestination: config.VerificationDestination,
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
