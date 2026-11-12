package checks

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/store/sqlite"
	"github.com/herbygillot/dockhand/internal/store/sqlite/sqlitetest"
)

// at is the tests' clock.
var at = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// sessions are a test's sessions, started as the engine starts them.
type sessions struct{ c *coord.Coordinator }

func (s sessions) Session(ctx context.Context, kind model.SessionKind) (*coord.Session, error) {
	return s.c.Start(ctx, kind, "test")
}

func (s sessions) Observing(ctx context.Context) (context.Context, *coord.Session, func(), error) {
	session, err := s.Session(ctx, model.SessionObserver)
	if err != nil {
		return ctx, nil, func() {}, err
	}
	return ctx, session, func() { session.End(context.WithoutCancel(ctx)) }, nil
}

// service is a check-execution service on a store of its own, with no
// providers, checkout, or hooks until a test gives them.
func service(t *testing.T) *Service {
	t.Helper()
	s, err := sqlite.Open(t.Context(), sqlitetest.Fresh(t, filepath.Join(t.TempDir(), "dockhand.db")), sqlite.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	repository, err := s.Register(t.Context(), "/src/macports-ports/.git")
	require.NoError(t, err)
	now := func() time.Time { return at }
	return &Service{Store: s, Repository: repository, Sessions: sessions{&coord.Coordinator{Store: s, Repository: repository}},
		Logs: t.TempDir(), Launches: t.TempDir(), Archives: &Archives{Directory: t.TempDir(), Store: s, Repository: repository, Now: now},
		Now: now, Poll: 10 * time.Millisecond}
}

// session is a foreground session of the service's, ended with the test.
func (c *Service) session(t *testing.T) *coord.Session {
	t.Helper()
	session, err := c.Sessions.Session(t.Context(), model.SessionForeground)
	require.NoError(t, err)
	t.Cleanup(func() { session.End(context.WithoutCancel(t.Context())) })
	return session
}

// queued records a branch of its own, named name, with a revision, and
// queues its check of jq in environments.
func (c *Service) queued(t *testing.T, name string, environments ...model.Environment) model.Run {
	t.Helper()
	commit := model.ObjectID("1111111111111111111111111111111111111111")
	branch := model.Branch{ID: model.BranchID(store.NewID("br")), Repository: c.Repository, Name: model.BranchPrefix + name, Base: commit, State: model.BranchOpen, CreatedAt: at}
	revision := model.Revision{ID: model.RevisionID(store.NewID("rev")), Branch: branch.ID, Kind: model.RevisionCommit, Head: commit, CreatedAt: at,
		Source: model.Source{Commit: commit, Tree: "2222222222222222222222222222222222222222", Base: commit}}
	require.NoError(t, c.Store.Update(t.Context(), c.Repository, func(tx store.Tx) error {
		if err := tx.AddBranch(branch); err != nil {
			return err
		}
		return tx.AddRevision(revision)
	}))
	plan := model.Plan{ID: model.PlanID(store.NewID("plan")), Revision: revision.ID, Environments: environments, Tests: model.TestsSkip, CreatedAt: at,
		Targets: []model.PlanTarget{{ID: "jq", Target: model.Target{Name: "jq", Portfile: "textproc/jq/Portfile"}, Directory: "textproc/jq", Kind: model.Substantive, Role: model.Changed}}}
	for _, environment := range environments {
		plan.Builds = append(plan.Builds, model.EnvironmentPlan{Environment: environment, Order: []model.TargetID{"jq"}})
	}
	run, err := c.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	return run
}
