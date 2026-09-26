package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

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

func (p xcodeOnArm) Ports(ctx context.Context, source model.Source, directory string, platform model.Platform) ([]macports.PortInfo, error) {
	ports, err := p.fakePorts.Ports(ctx, source, directory, platform)
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
		case ports[i].Name == "libharbor" && platform.Architecture == "arm64":
			options["use_xcode"] = "yes"
		case ports[i].Name == "harbor-tools" && p.broken:
			options["use_xcode"] = "sometimes"
		}
		ports[i].Options = options
	}
	return ports, nil
}

// xcodeSkipper stands in for Tart without an Xcode image: it skips what
// needs Xcode, and what depends on it, and builds the rest.
type xcodeSkipper struct{ scriptedProvider }

func (p *xcodeSkipper) Skips(_ context.Context, plan model.Plan, environment model.Environment) ([]Skip, error) {
	var skips []Skip
	for _, target := range plan.Targets {
		if target.NeedsXcodeOn(environment.Platform) {
			skips = append(skips, Skip{Target: target.ID, Environment: environment, Reason: "needs Xcode", Remedy: "make an Xcode image"})
		}
	}
	return skips, nil
}

func (p *xcodeSkipper) Execute(ctx context.Context, job Job, build Build) error {
	own, _ := p.Skips(ctx, job.Plan, job.Environment)
	skipped := map[model.TargetID]bool{}
	for _, skip := range SkipDependents(job.Plan, job.Environment, job.Targets, own) {
		if err := build.Record(model.TargetResult{Target: skip.Target, Outcome: model.OutcomeNotRun, Tests: model.TestsNone, Detail: skip.Reason}); err != nil {
			return err
		}
		skipped[skip.Target] = true
	}
	var rest []model.PlanTarget
	for _, target := range job.Targets {
		if !skipped[target.ID] {
			rest = append(rest, target)
		}
	}
	job.Targets = rest
	return p.scriptedProvider.Execute(ctx, job, build)
}

// A target needs Xcode where its own evaluation says so, per release. A
// provider that can't build it skips it and what depends on it, says why
// before the check and after, and the run needs attention rather than
// failing: nothing was built and failed.
func TestATargetThatNeedsXcodeIsSkippedWhereItCantBeBuilt(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	revision := harborBranch(t, e)
	e.PortReader = xcodeOnArm{fakePorts: harborPorts()}

	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm, tahoeX86}})
	require.NoError(t, err)
	library, _ := plan.Target("libharbor")
	require.Equal(t, []model.Platform{tahoeArm.Platform}, library.NeedsXcode)
	viewer, _ := plan.Target("harbor-viewer")
	require.Empty(t, viewer.NeedsXcode)

	provider := &xcodeSkipper{}
	e.Providers = map[string]Provider{"command": provider}
	skips, err := e.Skips(t.Context(), plan)
	require.NoError(t, err)
	var words []string
	for _, skip := range skips {
		words = append(words, string(skip.Target)+"@"+skip.Environment.Platform.Architecture+": "+skip.Reason)
	}
	require.Equal(t, []string{
		"libharbor@arm64: needs Xcode",
		"harbor-cli@arm64: needs libharbor, which isn't built",
		"harbor-viewer@arm64: needs libharbor, which isn't built",
	}, words, "harbor-viewer-legacy is excluded on arm64; x86_64 builds everything")
	require.Equal(t, "make an Xcode image", skips[0].Remedy)
	require.Equal(t, model.TargetID("libharbor"), skips[2].Because)

	var branch model.Branch
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		branch, err = r.Branch(revision.Branch)
		return err
	}))
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	got := outcomes(t, e, run)
	require.Equal(t, model.OutcomeNotRun, got["libharbor@arm64"])
	require.Equal(t, model.OutcomeNotRun, got["harbor-viewer@arm64"], "not built against an old libharbor, nor counted as failed")
	require.Equal(t, model.OutcomePassed, got["libharbor@x86_64"])
	require.Equal(t, model.RunAttention, run.State, "nothing failed")
	require.Contains(t, run.Detail, "libharbor not run on command")
	require.Contains(t, run.Detail, ": needs Xcode")

	evidence, err := e.RunEvidence(t.Context(), run.ID)
	require.NoError(t, err)
	for _, target := range evidence.Targets {
		if target.Target.ID == "libharbor" {
			require.Equal(t, "· not run: needs Xcode", TargetWords(plan, target.Target, tahoeArm, target.Outcomes[0], false))
		}
	}

	e.PortReader = xcodeOnArm{fakePorts: harborPorts(), broken: true}
	unresolved, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm}, Also: []string{"harbor-tools"}})
	require.NoError(t, err)
	require.False(t, unresolved.Runnable(), "a use_xcode that can't be read never counts as no")
	require.Contains(t, unresolved.Unresolved[0].Reason, `invalid use_xcode value "sometimes"`)
}
