package verify

import (
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestDependentPlanRejectsUnboundOrMissingRoots(t *testing.T) {
	source := record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}
	platform := record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	root := record.Target{Name: "root", Portfile: "devel/root/Portfile"}
	job := record.Job{ID: "job", Phase: record.PhaseVerification, Spec: record.JobSpec{Action: record.Verify, Source: source, Targets: []record.Target{root}, Destination: record.VerificationComplete, Verification: record.VerificationRequired, IncludeDependents: true, Build: &record.BuildConfig{Provider: "tart", Platform: platform, EnvironmentDigest: "image", VerifierDigest: "verifier", Tests: record.TestDeclared}}}
	evaluation := TargetEvaluation{Source: source, Platform: platform, Target: root}
	coverage := Coverage{Source: source, Platform: platform, Targets: []CoverageTarget{{Target: root, Root: true, Evaluation: &evaluation}}}
	for _, scenario := range []string{"source", "root", "duplicate", "evaluation", "evaluation-source", "evaluation-platform", "evaluation-target"} {
		t.Run(scenario, func(t *testing.T) {
			value := coverage
			value.Targets = append([]CoverageTarget{}, coverage.Targets...)
			switch scenario {
			case "source":
				value.Source.Tree = record.ObjectID(strings.Repeat("b", 40))
			case "root":
				value.Targets = nil
			case "duplicate":
				value.Targets = append(value.Targets, value.Targets[0])
			case "evaluation":
				value.Targets[0].Evaluation = nil
			case "evaluation-source", "evaluation-platform", "evaluation-target":
				changed := evaluation
				switch scenario {
				case "evaluation-source":
					changed.Source.Tree = record.ObjectID(strings.Repeat("b", 40))
				case "evaluation-platform":
					changed.Platform.Version = "other"
				case "evaluation-target":
					changed.Target.Name = "other"
				}
				value.Targets[0].Evaluation = &changed
			}
			plan, err := PlanDependents(job, record.Revision{}, value)
			if strings.HasPrefix(scenario, "evaluation") {
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
