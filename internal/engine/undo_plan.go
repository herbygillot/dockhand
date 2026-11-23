package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
)

// UndoPlan describes what undo would restore without making the restoration.
type UndoPlan struct {
	Checkpoint *model.Checkpoint
	Branch     model.Branch
	Run        *model.Run
	Prefixes   []buildenv.PutBackPlan
}

// PlanUndo reads a checkpoint or check's restoration record without changing it.
func (e *Engine) PlanUndo(ctx context.Context, name string) (UndoPlan, error) {
	if model.IsRunName(name) {
		run, err := e.RunNamed(ctx, name)
		if err != nil {
			return UndoPlan{}, err
		}
		plan := UndoPlan{Run: &run}
		for _, provider := range e.Providers {
			planner, ok := provider.(buildenv.PutBackPlanner)
			if !ok {
				continue
			}
			prefix, found, err := planner.PlanPutBack(ctx, run.Name())
			if err != nil {
				return UndoPlan{}, err
			}
			if found {
				plan.Prefixes = append(plan.Prefixes, prefix)
			}
		}
		if len(plan.Prefixes) == 0 {
			return UndoPlan{}, fmt.Errorf("%s has no recorded changes to put back", run.Name())
		}
		return plan, nil
	}
	kind, digits, _ := strings.Cut(name, "-")
	number, err := strconv.Atoi(digits)
	if err != nil || !named(kind) {
		return UndoPlan{}, fmt.Errorf("%q is not a checkpoint name, such as tidy-3", name)
	}
	checkpoint, branch, err := e.checkpointNamed(ctx, name, kind, number)
	if err != nil {
		return UndoPlan{}, err
	}
	return UndoPlan{Checkpoint: &checkpoint, Branch: branch}, nil
}
