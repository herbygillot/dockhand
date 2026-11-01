package checks

import (
	"context"
	"slices"

	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// Recovery is the check-execution service's for a run: one left running
// by a driver that's gone is a candidate serve takes first (Candidates).
// A branch serve prepared and never queued a check of is the engine's to
// recover, since its check has to be planned first (Engine.ResumePrepared).

// Candidates are the runs serve could drive, in the order it takes them:
// a run left running by a driver that is gone, then queued runs a person
// asked for, then serve's own, oldest first. Runs a live session holds,
// this one included, are left out.
func (c *Service) Candidates(ctx context.Context, session *coord.Session) ([]model.Run, error) {
	var runs []model.Run
	var holders map[string]*model.Session
	// The runs and their holders in one read (SQL-O4), every 2 s in serve.
	if err := c.Store.View(ctx, c.Repository, func(r store.Reader) error {
		var err error
		if runs, err = r.Runs(store.RunFilter{States: []model.RunState{model.RunQueued, model.RunRunning}}); err != nil {
			return err
		}
		holders, err = session.Holders(r, RunResources(runs)...)
		return err
	}); err != nil {
		return nil, err
	}
	rank := func(run model.Run) int {
		switch {
		case run.State == model.RunRunning:
			return 0
		case run.Origin == model.OriginPerson:
			return 1
		}
		return 2
	}
	slices.SortStableFunc(runs, func(a, b model.Run) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		return a.Number - b.Number
	})
	var candidates []model.Run
	for _, run := range runs {
		if holders[RunResource(run.ID)] == nil {
			candidates = append(candidates, run)
		}
	}
	return candidates, nil
}
