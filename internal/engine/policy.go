package engine

import (
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/provider"
)

// PolicyNotes say where a plan's test policy can't be carried out as
// asked, for the plan's preview and its check's heading.
func (e *Engine) PolicyNotes(plan model.Plan) []string {
	if plan.Tests != model.TestsSkip {
		return nil
	}
	var notes, seen []string
	for _, environment := range plan.Environments {
		name := environment.Provider
		if slices.Contains(seen, name) {
			continue
		}
		seen = append(seen, name)
		if own, ok := e.Providers[name].(provider.OwnTestsProvider); ok && own.RunsOwnTests() {
			notes = append(notes, fmt.Sprintf("%s runs its workflow's own tests; with --tests skip they run there, and don't count", name))
		}
	}
	return notes
}
