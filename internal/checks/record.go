package checks

import (
	"context"

	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// LoadCheck loads a check's record, as evidence reads it, in the store's
// one read of a check (the SQL study's O2): its plan, its executions and
// their results, the executions that built the results it reused, with
// their checks, and, where sources asks, what its results' builds read.
// The run is the caller's, which a driver finishing it holds ahead of its
// record.
func LoadCheck(r store.Reader, run model.Run, sources bool) (evidence.Check, error) {
	record, err := r.Check(run.ID)
	if err != nil {
		return evidence.Check{}, err
	}
	check := evidence.Check{Run: run, Plan: record.Plan, Executions: record.Executions, Results: record.Results}
	for id, origin := range record.Origins {
		if check.Origins == nil {
			check.Origins = map[model.ExecutionID]evidence.Origin{}
		}
		check.Origins[id] = evidence.Origin{Execution: origin.Execution, Check: origin.Run.Name()}
	}
	if sources && len(record.Inputs) > 0 {
		check.Inputs = record.Inputs
	}
	return check, nil
}

// Stopped reports whether a run recorded as running has no live process
// driving it: the process running it ended without settling it, as a
// killed foreground check does. It is resumable: the next serve takes it
// up where it stopped, and so does dockhand wait; cancel ends it. Judging
// who is alive takes a session.
func Stopped(ctx context.Context, session *coord.Session, run model.Run) (bool, error) {
	if run.State != model.RunRunning {
		return false, nil
	}
	holder, err := session.Holder(ctx, RunResource(run.ID))
	return err == nil && holder == nil, err
}
