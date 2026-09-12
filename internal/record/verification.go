package record

// ArtifactRequirement names an output needed from another planned target.
// It must resolve to a concrete Artifact before submission in a BuildSpec.
type ArtifactRequirement struct {
	Producer TargetID
	Name     string
}

// VerificationTarget describes one unit of requested coverage and the work
// or outputs that must be available before it can execute.
type VerificationTarget struct {
	ID       TargetID
	Port     Target
	Platform Platform
	// Prerequisites lists predecessor targets in the same plan.
	Prerequisites []TargetID
	Inputs        []ArtifactRequirement
}

// VerificationPlan describes target and configuration coverage for one revision.
// Targets may refer to future outputs; an Attempt's BuildSpec holds only
// resolved inputs. Planning coverage does not establish provider admission.
type VerificationPlan struct {
	JobID      JobID
	RevisionID RevisionID
	Targets    []VerificationTarget
}
