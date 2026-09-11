package model

type ArtifactRequirement struct {
	Producer TargetID
	Name     string
}

type VerificationTarget struct {
	ID            TargetID
	Port          Target
	Platform      Platform
	Prerequisites []TargetID
	Inputs        []ArtifactRequirement
}

// Planned targets can refer to future artifacts; submitted BuildSpecs cannot.
type VerificationPlan struct {
	JobID      JobID
	RevisionID RevisionID
	Targets    []VerificationTarget
}
