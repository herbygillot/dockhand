package workflow_test

import (
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

// continuationFixture is a published contribution whose port master carries
// at masterVersion, and a prior bump that recorded moving it from 1 to 2.
func continuationFixture(t *testing.T, masterVersion string) (*fixture, *publicationForge, record.Job, record.Source) {
	t.Helper()
	f, hosting, _ := publishedLifecycleFixture(t)
	master := commitPort(t, f, "upstream-master", "version "+masterVersion+"\n")
	master.Base = master.Commit
	ports := &boundPorts{}
	ports.snapshot = func(c macports.Context) map[string]macports.PortInfo {
		version := "1"
		if c.Source().Tree == master.Tree {
			version = masterVersion
		}
		return map[string]macports.PortInfo{"fixture": {Name: "fixture", Version: version}}
	}
	f.engine.Ports = ports
	f.engine.Repo = f.repo
	prior := record.Job{ChangeID: "change", Spec: record.JobSpec{Targets: []record.Target{{Name: "fixture", Portfile: "devel/fixture/Portfile"}}},
		ResolvedRelease: &record.Release{CurrentVersion: "1", Version: "2"}}
	return f, hosting, prior, master
}

var continuationPlatform = record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}

// A bump reads master and the PR before continuing an open contribution.
// Master untouched and the PR open is the only picture that continues; a
// merged PR retires the contribution and a new update starts; everything
// else stops and says what it found, for a person to decide.
func TestContinuationReadsMasterAndThePullRequestFirst(t *testing.T) {
	t.Parallel()
	t.Run("untouched and open continues", func(t *testing.T) {
		t.Parallel()
		f, _, prior, master := continuationFixture(t, "1")
		decision, err := f.engine.CheckContinuation(t.Context(), prior, master, continuationPlatform)
		require.NoError(t, err)
		require.False(t, decision.Fresh)
		require.Contains(t, decision.Detail, "PR #")
		require.Contains(t, decision.Detail, "is open")
		require.Contains(t, decision.Detail, "master has fixture at 1")
	})
	t.Run("landed and open is a decision", func(t *testing.T) {
		t.Parallel()
		f, _, prior, master := continuationFixture(t, "2")
		_, err := f.engine.CheckContinuation(t.Context(), prior, master, continuationPlatform)
		require.ErrorIs(t, err, workflow.ErrContinuation)
		require.Contains(t, err.Error(), "fixture is already at 2 on master")
		require.Contains(t, err.Error(), "PR #")
		require.Contains(t, err.Error(), "abandon the contribution or close the PR")
	})
	t.Run("merged retires and starts anew", func(t *testing.T) {
		t.Parallel()
		f, hosting, prior, master := continuationFixture(t, "2")
		hosting.observation.PullRequest.State = record.PullRequestMerged
		decision, err := f.engine.CheckContinuation(t.Context(), prior, master, continuationPlatform)
		require.NoError(t, err)
		require.True(t, decision.Fresh)
		require.Contains(t, decision.Detail, "is merged")
		require.Contains(t, decision.Detail, "contribution retired")
		require.Contains(t, decision.Detail, "a new update starts from master")
		status, err := f.engine.Status(t.Context(), workflow.Scope{All: true})
		require.NoError(t, err)
		require.Equal(t, record.ChangeMerged, status.Changes[0].Disposition, "retired in the record, not only in the message")
	})
	t.Run("closed without merging stops", func(t *testing.T) {
		t.Parallel()
		f, hosting, prior, master := continuationFixture(t, "1")
		hosting.observation.PullRequest.State = record.PullRequestClosed
		_, err := f.engine.CheckContinuation(t.Context(), prior, master, continuationPlatform)
		require.ErrorIs(t, err, workflow.ErrContinuation)
		require.Contains(t, err.Error(), "is closed")
		require.Contains(t, err.Error(), "contribution retired")
		require.Contains(t, err.Error(), "bump again if a new update is wanted")
	})
	t.Run("moved by someone else stops", func(t *testing.T) {
		t.Parallel()
		f, _, prior, master := continuationFixture(t, "3")
		_, err := f.engine.CheckContinuation(t.Context(), prior, master, continuationPlatform)
		require.ErrorIs(t, err, workflow.ErrContinuation)
		require.Contains(t, err.Error(), "master has fixture at 3")
		require.Contains(t, err.Error(), "moved it from 1 to 2")
	})
	t.Run("an unreachable forge leaves the recorded state, and says so", func(t *testing.T) {
		t.Parallel()
		f, _, prior, master := continuationFixture(t, "2")
		f.engine.Accounts, f.engine.PullRequests = nil, nil
		_, err := f.engine.CheckContinuation(t.Context(), prior, master, continuationPlatform)
		require.ErrorIs(t, err, workflow.ErrContinuation)
		require.Contains(t, err.Error(), "as recorded, not re-checked")
		require.Contains(t, err.Error(), "already at 2 on master")
	})
	t.Run("no recorded release compares nothing and continues", func(t *testing.T) {
		t.Parallel()
		f, _, prior, master := continuationFixture(t, "3")
		prior.ResolvedRelease = nil
		decision, err := f.engine.CheckContinuation(t.Context(), prior, master, continuationPlatform)
		require.NoError(t, err)
		require.False(t, decision.Fresh)
	})
	t.Run("a master that cannot be evaluated stops", func(t *testing.T) {
		t.Parallel()
		f, _, prior, _ := continuationFixture(t, "1")
		bogus := record.Source{Tree: record.ObjectID(strings.Repeat("0", 40)), Commit: record.ObjectID(strings.Repeat("0", 40))}
		_, err := f.engine.CheckContinuation(t.Context(), prior, bogus, continuationPlatform)
		require.ErrorIs(t, err, workflow.ErrContinuation)
		require.Contains(t, err.Error(), "could not be evaluated on master")
	})
}
