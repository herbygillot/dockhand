package record

import "time"

// CommitIdentity is captured from Git configuration when preparation is requested.
type CommitIdentity struct {
	Name  string
	Email string
}

// EditIntent is what a person asked of the edit beyond the version: the
// choices that shape the commit. It is set once from the command line and
// travels unchanged through the job record to the editor, so an option is
// one field rather than one copied through every type on the way.
type EditIntent struct {
	// SharedRelease authorizes moving every subport that shares the
	// selected port's release source.
	SharedRelease bool `json:",omitempty"`
	// Stub names the port a person selected when the edit targets its newest
	// versioned subport instead: the contribution, branch, and commit carry
	// this name, and the editor honors the recorded redirect.
	Stub string `json:",omitempty"`
	// KeepOldChecksums refreshes a legacy checksum group's values in place,
	// md5 and sha1 included, instead of rewriting it as rmd160, sha256, and size.
	KeepOldChecksums bool `json:",omitempty"`
}

// PreparationSpec records the inputs a later driver needs to create a contribution.
type PreparationSpec struct {
	EditIntent
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
	Scope              *ReleaseScope `json:",omitempty"`
	Branch             string
	Source             Source
	IntegrationStarted bool
	// IntegratedAt is when the branch was integrated, the end of the
	// preparation phase; nil until it is.
	IntegratedAt *time.Time `json:",omitempty"`
	// PatchProblems names declared patches that no longer apply to the
	// candidate source. The branch is still created; verification is not
	// started until a person refreshes the patch.
	PatchProblems []string `json:",omitempty"`
}

// CorrectionSpec freezes the preconditions for replacing a contribution's
// branch head: the revision it replaces and the heads it expects. An amend
// or rebase carries its captured candidate; an update prepared onto the
// contribution's own revision carries none, since the job prepares the
// candidate itself through the same stages as a fresh preparation.
type CorrectionSpec struct {
	Scope        *ReleaseScope `json:",omitempty"`
	ChangeID     ChangeID
	RevisionID   RevisionID
	Branch       string
	PreviousHead ObjectID
	RemoteHead   ObjectID
	Candidate    Source `json:",omitempty"`
}
