package model

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"slices"
)

// ObjectID is a full Git object identifier for a commit, tree, or blob.
type ObjectID string

// RepositoryID identifies a registered ports repository: one clone, with its
// linked worktrees.
type RepositoryID string

// Digest is the hex SHA-256 of data, the form in which records and the files
// beside them identify content.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Source identifies the complete ports-tree snapshot used by an operation.
type Source struct {
	// Commit identifies the source commit, when the snapshot has one.
	Commit ObjectID
	// Tree identifies the immutable root tree, including shared PortGroups.
	Tree ObjectID
	// Base identifies the upstream base commit, when known.
	Base ObjectID
}

// Platform identifies the operating system, release, and CPU architecture
// requested for evaluation or verification.
type Platform struct {
	OS           string
	Version      string
	Architecture string
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
