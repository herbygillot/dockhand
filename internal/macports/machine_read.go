package macports

import "context"

// MachineReadExperiment contains evidence about original declaration indices.
// It deliberately carries no second observation, plan, value, or error text.
type MachineReadExperiment struct {
	Outcome        string
	Varied         []int
	Field          string   `json:",omitempty"`
	ErrorCode      string   `json:",omitempty"`
	BuildOnlyMoved []string `json:",omitempty"`
}

// MachineReadExperimenter returns only summaries of private fresh evaluations.
// Candidates name original declaration indices; acted lists compared options.
type MachineReadExperimenter interface {
	MachineReadExperiments(context.Context, Context, ObservationRequest, Observation, map[string][]int, []string) map[string]MachineReadExperiment
}
