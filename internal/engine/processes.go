package engine

import (
	"context"

	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// Process is a dockhand process working on the repository now: its
// session, alive by coord's liveness, whether it leads the queue, the
// checks it runs, and the schema it expects where that isn't the
// database's (0 where it matches, or its build recorded none).
type Process struct {
	Session model.Session
	Leads   bool
	Runs    []model.Run
	// Expects is the schema the process's build writes where the database
	// is at another, Database: a process on an older schema can't record
	// what it does after a migration.
	Expects, Database int
}

// Processes are the dockhand processes working on the repository, oldest
// first, but session's own, whose process asks: those with a session,
// serve, a check, and status among them, and not commands that open none,
// such as submit and update.
func (e *Engine) Processes(ctx context.Context, session *coord.Session) ([]Process, error) {
	var processes []Process
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		sessions, err := r.OpenSessions()
		if err != nil {
			return err
		}
		database := 0
		for _, other := range sessions {
			if other.ID == session.ID() {
				database = other.Schema
			}
		}
		runs, err := r.Runs(store.RunFilter{States: []model.RunState{model.RunRunning}})
		if err != nil {
			return err
		}
		holders, err := session.Holders(r, append(runResources(runs), coord.LeaderResource)...)
		if err != nil {
			return err
		}
		for _, other := range sessions {
			if other.ID == session.ID() {
				continue
			}
			verdict, err := session.Judge(other)
			if err != nil {
				return err
			}
			if verdict.Dead {
				continue
			}
			process := Process{Session: other}
			if leader := holders[coord.LeaderResource]; leader != nil && leader.ID == other.ID {
				process.Leads = true
			}
			for _, run := range runs {
				if holder := holders[RunResource(run.ID)]; holder != nil && holder.ID == other.ID {
					process.Runs = append(process.Runs, run)
				}
			}
			if other.Schema != 0 && database != 0 && other.Schema != database {
				process.Expects, process.Database = other.Schema, database
			}
			processes = append(processes, process)
		}
		return nil
	})
	return processes, err
}
