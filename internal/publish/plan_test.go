package publish_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

// destinationForge resolves repository names from remote paths without a network.
type destinationForge struct {
	login        string
	repositories map[string]forge.RepositoryInfo
}

func (d *destinationForge) Name() string                                      { return "fixture" }
func (d *destinationForge) Authenticate(context.Context) error                { return nil }
func (d *destinationForge) AuthenticatedUser(context.Context) (string, error) { return d.login, nil }
func (d *destinationForge) NameFromRemote(remote string) (string, error) {
	name := strings.TrimSuffix(filepath.Base(filepath.Dir(remote))+"/"+filepath.Base(remote), ".git")
	if _, ok := d.repositories[name]; !ok {
		return "", fmt.Errorf("unexpected remote %q", remote)
	}
	return name, nil
}
func (d *destinationForge) RepositoryInfo(_ context.Context, name string) (forge.RepositoryInfo, error) {
	info, ok := d.repositories[name]
	if !ok {
		return forge.RepositoryInfo{}, forge.ErrNotFound
	}
	return info, nil
}
func (d *destinationForge) Find(context.Context, forge.PullRequestQuery) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, nil
}
func (d *destinationForge) Observe(context.Context, record.PullRequestRef) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, nil
}
func (d *destinationForge) Create(context.Context, forge.PullRequestInput) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, nil
}
func (d *destinationForge) Update(context.Context, forge.PullRequestInput) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, nil
}

// destinationFixture models a checkout whose origin is the upstream repository
// and whose fork is a differently named remote.
func destinationFixture(t *testing.T) (*publish.Service, *destinationForge) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	remotes := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	run("init", "-q", "-b", "main")
	upstream := filepath.Join(remotes, "macports", "macports-ports.git")
	fork := filepath.Join(remotes, "contributor", "macports-ports.git")
	other := filepath.Join(remotes, "colleague", "macports-ports.git")
	run("remote", "add", "origin", upstream)
	run("remote", "add", "contributor", fork)
	run("remote", "add", "colleague", other)
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	hosting := &destinationForge{login: "Contributor", repositories: map[string]forge.RepositoryInfo{
		"macports/macports-ports":    {Name: "macports/macports-ports", DefaultBranch: "master", CloneURL: upstream},
		"contributor/macports-ports": {Name: "contributor/macports-ports", DefaultBranch: "master", Parent: "macports/macports-ports", CloneURL: fork},
		"colleague/macports-ports":   {Name: "colleague/macports-ports", DefaultBranch: "master", Parent: "macports/macports-ports", CloneURL: other},
	}}
	return &publish.Service{Repo: repo, Forge: hosting, LockDirectory: filepath.Join(t.TempDir(), "locks")}, hosting
}

func TestDestinationRefusesPushingToARepositoryTheUserDoesNotOwn(t *testing.T) {
	s, _ := destinationFixture(t)
	_, err := s.Destination(t.Context(), publish.Options{})
	require.ErrorIs(t, err, publish.ErrPrecondition)
	require.ErrorContains(t, err, `remote "origin" pushes to macports/macports-ports, which Contributor does not own`)
	require.ErrorContains(t, err, "select your fork with --remote: contributor (contributor/macports-ports)")
	require.NotContains(t, err.Error(), "colleague")

	_, err = s.Destination(t.Context(), publish.Options{Remote: "colleague"})
	require.ErrorIs(t, err, publish.ErrPrecondition)
	require.ErrorContains(t, err, `remote "colleague" pushes to colleague/macports-ports`)
}

func TestDestinationPublishesFromTheOwnedFork(t *testing.T) {
	s, hosting := destinationFixture(t)
	destination, err := s.Destination(t.Context(), publish.Options{Remote: "contributor"})
	require.NoError(t, err)
	require.Equal(t, "contributor/macports-ports", destination.HeadRepository)
	require.Equal(t, "macports/macports-ports", destination.Repository)
	require.Equal(t, "master", destination.BaseBranch)
	require.Equal(t, hosting.repositories["contributor/macports-ports"].CloneURL, destination.PushURL)
	require.Equal(t, hosting.repositories["macports/macports-ports"].CloneURL, destination.BaseURL)
}

func TestDestinationHintsWhenNoRemoteNamesTheFork(t *testing.T) {
	s, hosting := destinationFixture(t)
	hosting.login = "stranger"
	_, err := s.Destination(t.Context(), publish.Options{})
	require.ErrorIs(t, err, publish.ErrPrecondition)
	require.ErrorContains(t, err, "add a Git remote for your fork and select it with --remote")
}

func TestPlanToRefusesARecordedDestinationTheUserDoesNotOwn(t *testing.T) {
	s, hosting := destinationFixture(t)
	destination, err := s.Destination(t.Context(), publish.Options{Remote: "contributor"})
	require.NoError(t, err)
	hosting.login = "someone-else"
	_, err = s.PlanTo(t.Context(), record.Change{}, record.Source{}, record.Attempt{}, nil, destination)
	require.ErrorIs(t, err, publish.ErrPrecondition)
	require.ErrorContains(t, err, "recorded publication destination pushes to contributor/macports-ports, which someone-else does not own")
}
