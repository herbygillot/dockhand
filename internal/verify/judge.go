package verify

import (
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/v2/internal/record"
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
			if step.Verdict != record.VerdictPassed {
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
		environment = &value
	}
	evidence := record.Evidence{Verdict: verdict, Environment: environment, Steps: slices.Clone(observation.Steps), Artifacts: slices.Clone(observation.Artifacts), Logs: slices.Clone(observation.Logs), ObservedAt: observation.ObservedAt}
	if observation.Failure != nil {
		failure := *observation.Failure
		failure.DependencyChain = slices.Clone(failure.DependencyChain)
		evidence.Failure = &failure
	}
	return evidence, nil
}
