package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/workflow/choice"
	"maps"
	"net/http"
	"path/filepath"
	"time"

	forgegithub "github.com/herbygillot/dockhand/internal/forge/github"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macports/dependency"
	"github.com/herbygillot/dockhand/internal/macports/selection"
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
	TargetImages            map[string]string
	VerificationProvider    string
	VerificationDestination publish.Options
	DependencyTools         dependency.Tools
	DBPath                  string
	Repository              string
	GitExecutable           string
	TclExecutable           string
	MacPortsPrefix          string
	// IndexCacheDirectory overrides the shared PortIndex cache root; empty
	// selects DOCKHAND_INDEX_CACHE, then the user cache directory.
	IndexCacheDirectory string
	// IndexMirror overrides the mirror a cold cache seeds its index from,
	// as the tarballs directory the platform indexes are named under;
	// empty selects DOCKHAND_INDEX_MIRROR, then the default mirror.
	IndexMirror string
	Tart        tart.Config
	GitHub      github.Config
}

type Services struct {
	Workflow    *workflow.Engine
	Processes   *proc.Manager
	Preparation *preparation.Service
	close       func() error
	// providers is the provider choice the bindings resolve builds with.
	providers         choice.Providers
	ports             *selection.Reader
	providerName      string
	githubClient      *github.Client
	githubDestination publish.Options
}

func Build(ctx context.Context, config Config) (*Services, error) {
	if config.VerificationProvider != "" && config.VerificationProvider != "auto" && config.VerificationProvider != verify.ProviderTart && config.VerificationProvider != verify.ProviderGitHub {
		return nil, fmt.Errorf("unknown verification provider %q", config.VerificationProvider)
	}
	repo, err := openPortsTree(ctx, config.Repository, config.GitExecutable)
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

	ports := portReader(config, repo, indexMirror(config))
	githubClient := newGitHubClient(config.GitHub)
	discovery := releaseDiscovery(ports, githubClient, http.DefaultClient, config.GitExecutable)
	// One workspace per source for the command: Tart staging and dependent
	// discovery share the prepared tree rather than materializing it twice.
	workspaces := &workspace.Registry{}
	preparation := &preparation.Service{Repo: repo, Ports: ports, Upstream: discovery, DependencyTools: config.DependencyTools, Workspaces: workspaces}
	if config.Tart.ArtifactDirectory == "" {
		config.Tart.ArtifactDirectory = filepath.Join(filepath.Dir(store.Path()), "artifacts", "tart")
	}
	if config.Tart.PortIndexExecutable == "" && config.MacPortsPrefix != "" {
		config.Tart.PortIndexExecutable = filepath.Join(config.MacPortsPrefix, "bin", "portindex")
	}
	indexCache, err := indexCacheDirectory(config)
	if err != nil {
		store.Close()
		return nil, err
	}
	provider := &tart.Provider{Config: config.Tart, IndexCache: indexCache, State: store, Repository: repository.ID, Repo: repo, Workspaces: workspaces}
	githubProvider := &githubverify.Provider{State: store, Repository: repository.ID, Repo: repo, Directory: filepath.Join(filepath.Dir(store.Path()), "github-verification"), Client: githubClient}

	engine := &workflow.Engine{
		State:      store,
		Repository: repository.ID,
		Repo:       repo,
		Ports:      ports,
		Workspaces: workspaces,
		Preparer:   preparation,
		Dependents: dependentDiscovery{repo: repo, ports: ports, indexCache: indexCache, mirror: indexMirror(config), workspaces: workspaces},
		Releases:   preparation,
		Provider:   provider,
		Providers:  map[string]verify.Provider{verify.ProviderTart: provider, verify.ProviderGitHub: githubProvider},
		Publisher:  &publish.Service{Repo: repo, Forge: &forgegithub.Client{Client: githubClient}, LockDirectory: filepath.Join(filepath.Dir(store.Path()), "publication-locks"), Upstream: macports.PortsRepository},
		Now:        time.Now,
	}
	services := &Services{
		Workflow:          engine,
		Processes:         &proc.Manager{},
		Preparation:       preparation,
		close:             func() error { return errors.Join(workspaces.Close(), store.Close()) },
		ports:             ports,
		providerName:      config.VerificationProvider,
		githubClient:      githubClient,
		githubDestination: config.VerificationDestination,
	}
	services.providers = choice.Providers{Name: config.VerificationProvider, Local: provider, Remote: remoteBuild{services}, TargetImages: maps.Clone(config.TargetImages)}
	return services, nil
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
	repo, err := openPortsTree(ctx, config.Repository, config.GitExecutable)
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

// TestTimeout is how long a port's tests may run under this configuration:
// the configured bound, or Tart's default when none is set. The CLI derives
// its help from this rather than from the provider.
func (c Config) TestTimeout() time.Duration {
	if c.Tart.TestTimeout <= 0 {
		return tart.DefaultTestTimeout
	}
	return c.Tart.TestTimeout
}
