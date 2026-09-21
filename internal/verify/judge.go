package verify

import (
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/record"
)

func Judge(observation Observation) (record.Evidence, error) {
	if observation.ObservedAt.IsZero() {
		return record.Evidence{}, fmt.Errorf("verify: observation time is required")
	}
	verdict := observation.Verdict
	switch observation.State {
	case record.AttemptRunning:
		if verdict != "" && verdict != record.VerdictUnknown {
			return record.Evidence{}, fmt.Errorf("verify: running observation cannot have a terminal verdict")
		}
		verdict = record.VerdictUnknown
	case record.AttemptCanceled:
		if verdict != record.VerdictCanceled {
			return record.Evidence{}, fmt.Errorf("verify: canceled observation requires a canceled verdict")
		}
	case record.AttemptFinished:
		switch verdict {
		case record.VerdictPassed, record.VerdictFailed, record.VerdictBlocked, record.VerdictErrored, record.VerdictUnsupported:
		default:
			return record.Evidence{}, fmt.Errorf("verify: finished observation requires an explicit terminal verdict")
		}
	default:
		return record.Evidence{}, fmt.Errorf("verify: unsupported observation state %q", observation.State)
	}
	if verdict == record.VerdictPassed {
		if observation.Failure != nil {
			return record.Evidence{}, fmt.Errorf("verify: passing observation includes a failure")
		}
		for _, step := range observation.Steps {
			if step.Verdict != record.VerdictPassed && !advisoryFailure(step, observation.TestFailure) {
				return record.Evidence{}, fmt.Errorf("verify: passing observation contains a non-passing step")
			}
		}
	}
	var environment *record.EnvironmentEvidence
	if observation.Environment != nil {
		value := *observation.Environment
		if value.Provider == "" || value.EnvironmentDigest == "" || value.CapabilityDigest == "" || observation.Run.Provider != "" && value.Provider != observation.Run.Provider {
			return record.Evidence{}, fmt.Errorf("verify: invalid environment evidence")
		}
		if value.Guest != nil {
			guest := *value.Guest
			value.Guest = &guest
		}
		environment = &value
	}
	var workflow *record.WorkflowEvidence
	if observation.Workflow != nil {
		value := *observation.Workflow
		if value.Repository == "" || value.Branch == "" || value.Commit == "" || value.RunID <= 0 || value.RunAttempt <= 0 || value.URL == "" {
			return record.Evidence{}, fmt.Errorf("verify: incomplete workflow evidence")
		}
		if verdict == record.VerdictPassed {
			if value.Conclusion != "success" || len(value.Jobs) == 0 {
				return record.Evidence{}, fmt.Errorf("verify: passing workflow evidence requires successful jobs")
			}
			for _, job := range value.Jobs {
				if job.Status != "completed" || job.Conclusion != "success" {
					return record.Evidence{}, fmt.Errorf("verify: passing workflow evidence contains an unsuccessful job")
				}
			}
		}
		value.Jobs = slices.Clone(value.Jobs)
		for i := range value.Jobs {
			value.Jobs[i].Labels = slices.Clone(value.Jobs[i].Labels)
		}
		workflow = &value
	}
	evidence := record.Evidence{Workflow: workflow, TestOmission: observation.TestOmission, TestFailure: observation.TestFailure, Verdict: verdict, Dockhand: observation.Dockhand, Environment: environment, Steps: slices.Clone(observation.Steps), Artifacts: slices.Clone(observation.Artifacts), Logs: slices.Clone(observation.Logs), ObservedAt: observation.ObservedAt}
	for i := range evidence.Steps {
		evidence.Steps[i].Command = slices.Clone(evidence.Steps[i].Command)
	}
	if observation.Failure != nil {
		failure := *observation.Failure
		failure.DependencyChain = slices.Clone(failure.DependencyChain)
		failure.Fetches = slices.Clone(failure.Fetches)
		evidence.Failure = &failure
	}
	return evidence, nil
}

// advisoryFailure is the one non-passing step a passing verdict may carry:
// the port's declared tests failed under a policy that made them advisory,
// which the observation says in TestFailure. The step keeps its failed
// verdict, since that is what happened, and the verdict stays passed, since
// that is what the policy means.
func advisoryFailure(step record.StepResult, testFailure string) bool {
	return step.Phase == "test" && step.Verdict == record.VerdictFailed && testFailure != ""
}
