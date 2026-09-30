package model

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
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

// DeveloperTools are the compilers and SDKs a build environment offers.
type DeveloperTools string

const (
	// DeveloperToolsCommandLine is the Command Line Tools alone.
	DeveloperToolsCommandLine DeveloperTools = "command-line-tools"
	// DeveloperToolsXcode is Xcode, with the Command Line Tools beside it.
	DeveloperToolsXcode DeveloperTools = "xcode"
)

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

// VariantSpec is a target's variant choices as MacPorts writes them on a
// command line, sorted by name: "+docs -tests"; empty for its defaults.
func (t Target) VariantSpec() string {
	names := make([]string, 0, len(t.Variants))
	for name := range t.Variants {
		names = append(names, name)
	}
	slices.Sort(names)
	words := make([]string, 0, len(names))
	for _, name := range names {
		sign := "-"
		if t.Variants[name] {
			sign = "+"
		}
		words = append(words, sign+name)
	}
	return strings.Join(words, " ")
}

// ID is a target's identity in a plan: its port's name, and its variant
// choices after it where it has any, "s2n-tls +tests", since a check may
// build one port with several.
func (t Target) ID() TargetID {
	if spec := t.VariantSpec(); spec != "" {
		return TargetID(t.Name + " " + spec)
	}
	return TargetID(t.Name)
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
