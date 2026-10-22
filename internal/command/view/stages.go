package view

import (
	"fmt"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// StageLine is a stage's one line at the default level, under its
// change's heading (term.Staged): "  ✓ committed  jq: update to 1.8.1".
func StageLine(mark, stage, words string) string {
	return fmt.Sprintf("  %s %-10s %s", mark, stage, words)
}

// UpdateHeading is a version update's heading over its stage lines, the
// port as the person named it: "jq 1.7.1 → 1.8.1 (GitHub tag jq-1.8.1),
// on branch jq-1.8.1".
func UpdateHeading(named string, update engine.Update, branch model.Branch) string {
	source := ""
	if update.Release != nil && engine.ReleaseSource(*update.Release) != "" {
		source = " (" + engine.ReleaseSource(*update.Release) + ")"
	}
	return fmt.Sprintf("%s %s → %s%s, on branch %s", named, update.Before, update.After, source, branch.ShortName())
}
