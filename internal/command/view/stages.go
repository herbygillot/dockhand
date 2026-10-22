package view

import (
	"fmt"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
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

// PassedOn says where a check that passed built, by the environments'
// headings: "passed on macOS 15 and macOS 26".
func PassedOn(checked engine.Evidence) string {
	var on []string
	for _, i := range checked.Checked() {
		on = append(on, engine.EnvironmentHeading(checked.Plan.Environments[i], checked.Plan.Environments))
	}
	if len(on) == 0 {
		return "passed"
	}
	return "passed on " + prose.And(on)
}

// Where is a branch's directory after its name, " in ~/Source/…", or
// nothing where the command runs in it.
func Where(places config.Places, dir string) string {
	if dir == "" || places.Here(dir) {
		return ""
	}
	return " in " + places.Tilde(dir)
}
