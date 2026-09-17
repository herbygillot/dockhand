package record

// CommitIdentity is captured from Git configuration when preparation is requested.
type CommitIdentity struct {
	Name  string
	Email string
}

// PreparationSpec records the inputs a later driver needs to create a contribution.
type PreparationSpec struct {
	SharedRelease bool            `json:",omitempty"`
	Correction    *CorrectionSpec `json:",omitempty"`
	SourceURL     string          `json:",omitempty"`
	SourceBranch  string
	Platform      Platform
	Author        CommitIdentity
	// VerificationProblem preserves setup failure without preventing branch creation.
	VerificationProblem string `json:",omitempty"`
}

// PreparedChange is a candidate checkpoint before branch integration. Once
// integration starts, recovery inspects this exact commit rather than preparing again.
type PreparedChange struct {
	Scope              *ReleaseScope `json:",omitempty"`
	Branch             string
	Source             Source
	IntegrationStarted bool
	// PatchProblems names declared patches that no longer apply to the
	// candidate source. The branch is still created; verification is not
	// started until a person refreshes the patch.
	PatchProblems []string `json:",omitempty"`
}

// CorrectionSpec freezes branch adoption preconditions alongside its candidate.
type CorrectionSpec struct {
	Scope        *ReleaseScope `json:",omitempty"`
	ChangeID     ChangeID
	RevisionID   RevisionID
	Branch       string
	PreviousHead ObjectID
	RemoteHead   ObjectID
	Candidate    Source
}
