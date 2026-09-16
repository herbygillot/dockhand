package record

import (
	"cmp"
	"slices"
	"time"
)

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

// CompareTargets orders targets by their complete selection. Empty and nil
// variant maps represent the same selection.
func CompareTargets(a, b Target) int {
	for _, fields := range [][2]string{{a.Name, b.Name}, {a.Portfile, b.Portfile}, {a.Subport, b.Subport}} {
		if order := cmp.Compare(fields[0], fields[1]); order != 0 {
			return order
		}
	}
	aVariants := make([]string, 0, len(a.Variants))
	for name := range a.Variants {
		aVariants = append(aVariants, name)
	}
	bVariants := make([]string, 0, len(b.Variants))
	for name := range b.Variants {
		bVariants = append(bVariants, name)
	}
	slices.Sort(aVariants)
	slices.Sort(bVariants)
	for i := range min(len(aVariants), len(bVariants)) {
		if order := cmp.Compare(aVariants[i], bVariants[i]); order != 0 {
			return order
		}
		if a.Variants[aVariants[i]] != b.Variants[bVariants[i]] {
			if !a.Variants[aVariants[i]] {
				return -1
			}
			return 1
		}
	}
	return cmp.Compare(len(aVariants), len(bVariants))
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
	Scope    *ReleaseScope `json:",omitempty"`
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
