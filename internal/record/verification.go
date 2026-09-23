package record

// VerificationTarget describes one unit of requested coverage and the work
// or outputs that must be available before it can execute.
type VerificationTarget struct {
	// Coverage fields are populated by source-bound dependent planning.
	Root                bool       `json:",omitempty"`
	Build               *BuildSpec `json:",omitempty"`
	Problem             string     `json:",omitempty"`
	CoverageProblems    []string   `json:",omitempty"`
	Reasons             []string   `json:",omitempty"`
	IndexedDependencies []string   `json:",omitempty"`
	ID                  TargetID
	Port                Target
	Platform            Platform
}

// VerificationPlan describes target and configuration coverage for one job.
// RevisionID is empty for standalone source verification.
// Targets may refer to future outputs; an Attempt's BuildSpec holds only
// resolved inputs. Planning coverage does not establish provider admission.
type VerificationPlan struct {
	JobID      JobID
	RevisionID RevisionID
	Targets    []VerificationTarget
}
