package verify

import (
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/dependents"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestDependentPlanRejectsUnboundOrMissingRoots(t *testing.T) {
	source := record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}
	platform := record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	root := record.Target{Name: "root", Portfile: "devel/root/Portfile"}
	job := record.Job{ID: "job", Phase: record.PhaseVerification, Spec: record.JobSpec{Action: record.Verify, Source: source, Targets: []record.Target{root}, Destination: record.VerificationComplete, Verification: record.VerificationRequired, IncludeDependents: true, Build: &record.BuildConfig{Provider: "tart", Platform: platform, EnvironmentDigest: "image", VerifierDigest: "verifier", Tests: record.TestDeclared}}}
	evaluation := macports.Snapshot{Source: source, Platform: platform, Target: root, Ports: map[string]macports.PortInfo{"root": {Name: "root", Options: map[string]string{"use_xcode": "no"}}}}
	coverage := dependents.Coverage{Source: source, Platform: platform, Targets: []dependents.Candidate{{Target: root, Root: true, Evaluation: &evaluation}}}
	for _, scenario := range []string{"source", "root", "duplicate", "evaluation"} {
		t.Run(scenario, func(t *testing.T) {
			value := coverage
			value.Targets = append([]dependents.Candidate{}, coverage.Targets...)
			switch scenario {
			case "source":
				value.Source.Tree = record.ObjectID(strings.Repeat("b", 40))
			case "root":
				value.Targets = nil
			case "duplicate":
				value.Targets = append(value.Targets, value.Targets[0])
			case "evaluation":
				value.Targets[0].Evaluation = nil
			}
			plan, err := PlanDependents(job, record.Revision{}, value)
			if scenario == "evaluation" {
				require.NoError(t, err)
				require.NotEmpty(t, CoverageProblems(plan))
				require.Nil(t, plan.Targets[0].Build)
			} else {
				require.Error(t, err)
			}
		})
	}
	job.Spec.TargetBuilds = map[string]record.BuildConfig{"typo": *job.Spec.Build}
	_, err := PlanDependents(job, record.Revision{}, coverage)
	require.ErrorContains(t, err, "does not name a discovered dependent")
	job.Spec.TargetBuilds = map[string]record.BuildConfig{"root": *job.Spec.Build}
	_, err = PlanDependents(job, record.Revision{}, coverage)
	require.Error(t, err)
	wanted := record.BuildSpec{Source: source, Target: root, Config: *job.Spec.Build}
	previous := wanted
	previous.Preinstall = []record.Target{root}
	require.Contains(t, InputDifferences(wanted, previous), "preinstalled source roots differ")
	previous = wanted
	previous.Config.NeedsXcode = true
	require.Contains(t, InputDifferences(wanted, previous), "Xcode requirement differs")
}
