package view

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// A plan names the subport a branch changes, its siblings on a line of
// their own, built as MacPorts' CI builds every subport of a changed
// Portfile, and an Excluded line a reason and set of releases, its ports
// named together, where terraform-1.16's said "Changed 19 ports" and 25
// Excluded lines (field testing's terraform replay, 2026-10-22).
func TestAPlanNamesTheChangedSubportAndItsSiblings(t *testing.T) {
	t.Parallel()
	var targets []model.PlanTarget
	siblings := map[model.TargetID]bool{}
	var names []string
	for _, name := range []string{"terraform-1.16", "terraform_select", "terraform-1.0", "terraform-1.1", "terraform-1.2", "terraform-1.3"} {
		targets = append(targets, model.PlanTarget{ID: model.TargetID(name), Target: model.Target{Name: name}, Directory: "sysutils/terraform", Kind: model.Substantive, Role: model.Changed})
		if name != "terraform-1.16" {
			siblings[model.TargetID(name)] = true
			names = append(names, name)
		}
	}
	sequoia := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "24", Architecture: "arm64"}}
	tahoe := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	minmax := "minmax builds each port on its oldest and newest releases, macOS 12 and 27"
	var excluded []model.Exclusion
	for _, name := range names {
		excluded = append(excluded, model.Exclusion{Target: model.Target{Name: name}, Reason: minmax, Span: true})
	}
	plan := model.Plan{Targets: targets, Environments: []model.Environment{sequoia, tahoe}, Tests: model.TestsDeclared,
		Builds: []model.EnvironmentPlan{
			{Environment: sequoia, Exclusions: excluded, Order: []model.TargetID{"terraform-1.16"}},
			{Environment: tahoe, Exclusions: excluded, Order: []model.TargetID{"terraform-1.16"}},
		}}
	var out bytes.Buffer
	WritePlan(&out, plan, nil, siblings, nil)
	require.Contains(t, out.String(), "Changed     terraform-1.16\n")
	require.Contains(t, out.String(), "Siblings    terraform_select, terraform-1.0, terraform-1.1, terraform-1.2, terraform-1.3, built as MacPorts' CI builds every subport of a changed Portfile; --only narrows\n")
	require.Contains(t, out.String(), fmt.Sprintf("Excluded    terraform_select, terraform-1.0, terraform-1.1, terraform-1.2, terraform-1.3: %s\n", minmax))

	out.Reset()
	WritePlan(&out, plan, nil, nil, nil)
	require.Contains(t, out.String(), "Changed     terraform-1.16, terraform_select, terraform-1.0, terraform-1.1, terraform-1.2, terraform-1.3\n", "with no record, as before")
	require.NotContains(t, out.String(), "Siblings")
}

// A link finding is a notice, its mark !, and one that couldn't be read
// says nothing of them, unmarked, as one whose links are each declared.
func TestWriteLinks(t *testing.T) {
	var out bytes.Buffer
	WriteLinks(&out, []engine.LinkFinding{
		{Target: "harbor", Port: "oniguruma", Library: "/opt/local/lib/libonig.5.dylib", Binary: "/opt/local/bin/harbor", Libraries: 2},
		{Target: "harbor-cli", Unread: true},
		{Target: "crystal", Clean: true, Ports: 5},
		{Target: "jq", Clean: true},
	})
	require.Equal(t, "Links\n  ! harbor links libonig.5.dylib from oniguruma (and 1 more of its libraries), which it doesn't declare in depends_lib\n"+
		"    couldn't read what harbor-cli's build links, so whether it declares them isn't said\n"+
		"    crystal links 5 ports, each declared in depends_lib\n"+
		"    jq links no port's libraries\n", out.String())
}
