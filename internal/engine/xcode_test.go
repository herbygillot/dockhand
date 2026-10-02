package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// xcodeOnArm is the harbor ports with libharbor asking for Xcode on arm64
// alone, and harbor-tools with a use_xcode that isn't a boolean when
// broken is set.
type xcodeOnArm struct {
	fakePorts
	broken bool
}

func (p xcodeOnArm) Ports(ctx context.Context, source model.Source, directory string, environment model.Environment, variants map[string]bool) ([]macports.PortInfo, error) {
	ports, err := p.fakePorts.Ports(ctx, source, directory, environment, variants)
	if err != nil {
		return nil, err
	}
	ports = append([]macports.PortInfo(nil), ports...)
	for i := range ports {
		options := map[string]string{}
		for key, value := range ports[i].Options {
			options[key] = value
		}
		switch {
		case ports[i].Name == "libharbor" && environment.Platform.Architecture == "arm64":
			options["use_xcode"] = "yes"
		case ports[i].Name == "harbor-tools" && p.broken:
			options["use_xcode"] = "sometimes"
		}
		ports[i].Options = options
	}
	return ports, nil
}

// remedied is a provider that can say how to give an environment Xcode.
type remedied struct{ scriptedProvider }

func (p *remedied) Remedy(model.Unmet) string { return "make an Xcode image" }

// A target needs Xcode where its own evaluation says so, per release, and
// so does what the plan builds after it. An environment with the Command
// Line Tools alone doesn't build either: the plan says so when the check is
// accepted, its provider never sees them, and the run needs attention
// rather than failing, since nothing failed. One with Xcode builds them.
func TestATargetThatNeedsXcodeIsUnmetWithoutIt(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	revision := harborBranch(t, e)
	e.PortReader = xcodeOnArm{fakePorts: harborPorts()}
	armTools := tahoeArm
	armTools.DeveloperTools = model.DeveloperToolsCommandLine
	x86Xcode := tahoeX86
	x86Xcode.DeveloperTools = model.DeveloperToolsXcode

	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{armTools, x86Xcode}})
	require.NoError(t, err)
	require.True(t, plan.NeedsXcodeIn(armTools, "libharbor"))
	require.False(t, plan.NeedsXcodeIn(x86Xcode, "libharbor"))
	require.False(t, plan.NeedsXcodeIn(armTools, "harbor-viewer"), "its prerequisite needs Xcode; it doesn't itself")
	armPlan, _ := plan.In(armTools)
	x86Plan, _ := plan.In(x86Xcode)
	require.Equal(t, []model.Unmet{
		{Target: "libharbor", Environment: armTools, Needs: model.RequiresXcode},
		{Target: "harbor-cli", Environment: armTools, Needs: model.RequiresXcode, Through: "libharbor"},
		{Target: "harbor-viewer", Environment: armTools, Needs: model.RequiresXcode, Through: "libharbor"},
	}, armPlan.Unmet, "harbor-viewer-legacy is excluded on arm64")
	require.Empty(t, x86Plan.Unmet, "x86_64 has Xcode")
	require.True(t, plan.Runnable())

	provider := &remedied{}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	var branch model.Branch
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		branch, err = r.Branch(revision.Branch)
		return err
	}))
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	for _, job := range provider.jobs {
		require.Equal(t, x86Xcode, job.Environment, "arm64 has nothing it can build, so no job")
	}
	got := outcomes(t, e, run)
	require.Equal(t, model.OutcomeUnmet, got["libharbor@arm64"])
	require.Equal(t, model.OutcomeUnmet, got["harbor-viewer@arm64"], "not built against an old libharbor, nor counted as failed")
	require.Equal(t, model.OutcomePassed, got["libharbor@x86_64"])
	require.Equal(t, model.RunAttention, run.State, "nothing failed")
	require.Contains(t, run.Detail, "libharbor isn't built on command macOS 26 (Tahoe) arm64 with the Command Line Tools: it needs Xcode; make an Xcode image")

	evidence, err := e.RunEvidence(t.Context(), run.ID)
	require.NoError(t, err)
	for _, target := range evidence.Targets {
		if target.Target.ID == "harbor-cli" {
			require.Equal(t, "· not built: needs Xcode, through libharbor", EvidenceWords(evidence, target, 0, false))
		}
	}
	problems := publicationProblems(evidence, nil)
	require.Contains(t, problems, "libharbor needs Xcode, which command macOS 26 (Tahoe) arm64 with the Command Line Tools hasn't; a check with Xcode there builds it, or share the branch as a draft (--draft)")

	e.PortReader = xcodeOnArm{fakePorts: harborPorts(), broken: true}
	unresolved, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{armTools}, Also: []string{"harbor-tools"}})
	require.NoError(t, err)
	require.False(t, unresolved.Runnable(), "a use_xcode that can't be read never counts as no")
	require.Contains(t, unresolved.Unresolved[0].Reason, `invalid use_xcode value "sometimes"`)
}
