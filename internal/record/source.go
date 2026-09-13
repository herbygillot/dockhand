package record

import "time"

// Source identifies the complete ports-tree snapshot used by an operation.
// The selected port and execution platform are supplied separately.
type Source struct {
	// Commit identifies the source commit, when the snapshot has one.
	Commit ObjectID
	// Tree identifies the immutable root tree, including shared PortGroups.
	Tree ObjectID
	// Base identifies the upstream base commit, when known.
	Base ObjectID
}

// Target selects a port, optional subport, and variant choices within a Source.
type Target struct {
	Name string
	// Portfile is the Portfile path relative to the source tree root.
	Portfile string
	// Subport selects a subport defined by the Portfile when nonempty.
	Subport string
	// Variants records explicit choices: true enables a variant and false
	// disables it. An absent key leaves that variant unspecified.
	Variants map[string]bool
}

// Platform identifies the operating system, release, and CPU architecture
// requested for evaluation or verification.
type Platform struct {
	OS           string
	Version      string
	Architecture string
}

// Revision binds a change to an immutable Source. Later edits or rebases produce
// another revision while earlier jobs and evidence retain their original inputs.
type Revision struct {
	ID       RevisionID
	ChangeID ChangeID
	// Previous links to the preceding revision, or is empty for the first.
	Previous  RevisionID
	Source    Source
	CreatedAt time.Time
}

// Artifact identifies externally stored build output, input, or logs.
// The state store retains this reference rather than the artifact's contents.
type Artifact struct {
	// Name identifies the artifact within its producer's outputs.
	Name string
	// Digest identifies the artifact's content for verification and reuse.
	Digest string
	// Location is the provider-supplied address used to retrieve the content.
	Location  string
	MediaType string
}

// Checkout describes where a captured verification tree came from. Head is
// provenance, not a claim that its commit contains the captured edits.
type Checkout struct {
	Branch        string
	Head          ObjectID
	ModifiedFiles int
}
