package record

// CommitIdentity is captured from Git configuration when preparation is requested.
type CommitIdentity struct {
	Name  string
	Email string
}

// PreparationSpec records the inputs a later driver needs to create a contribution.
type PreparationSpec struct {
	Correction   *CorrectionSpec `json:",omitempty"`
	SourceURL    string          `json:",omitempty"`
	SourceBranch string
	Platform     Platform
	Author       CommitIdentity
	// VerificationProblem preserves setup failure without preventing branch creation.
	VerificationProblem string `json:",omitempty"`
}

// PreparedChange is a candidate checkpoint before branch integration. Once
// integration starts, recovery inspects this exact commit rather than preparing again.
type PreparedChange struct {
	Branch             string
	Source             Source
	IntegrationStarted bool
}

// CorrectionSpec freezes branch adoption preconditions alongside its candidate.
type CorrectionSpec struct {
	ChangeID     ChangeID
	RevisionID   RevisionID
	Branch       string
	PreviousHead ObjectID
	RemoteHead   ObjectID
	Candidate    Source
}
