package macports

import "context"

// MachineReadExperiment contains evidence about original declaration indices.
// It deliberately carries no second observation, plan, value, or error text.
type MachineReadExperiment struct {
	Outcome        string
	Varied         []int
	Unexplained    []int    `json:",omitempty"`
	Field          string   `json:",omitempty"`
	ErrorCode      string   `json:",omitempty"`
	BuildOnlyMoved []string `json:",omitempty"`
}

// MachineReadCandidate names an original read and its recognized default chain.
// Defaults are option names, never evaluated values.
type MachineReadCandidate struct {
	Read     int
	Defaults []string
}

// MachineReadExperimenter returns only summaries of private fresh evaluations.
// Candidates name original declaration indices; acted lists compared options.
type MachineReadExperimenter interface {
	MachineReadExperiments(context.Context, Context, ObservationRequest, Observation, map[string][]MachineReadCandidate, []string) map[string]MachineReadExperiment
}
