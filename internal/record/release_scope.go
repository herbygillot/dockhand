package record

import (
	"errors"
	"fmt"
	"maps"
)

// ReleaseScope records the complete effect of one source-bound version input.
// The initiating target remains the user-facing contribution handle.
type ReleaseScope struct {
	Input     ReleaseInput
	Affected  []ReleaseMember
	Protected []ReleaseMember
}

// ReleaseInput locates the literal whose native evaluation established the scope.
type ReleaseInput struct {
	Portfile      string
	Offset        int
	Before, After string
}

// ReleaseMember retains source identity as well as the evaluated package version.
type ReleaseMember struct {
	NeedsXcode    bool
	Target        Target
	Before, After ReleaseState
	MetadataOnly  bool
}

// ReleaseState is the metadata preserved for scope checks after human edits.
type ReleaseState struct {
	MasterSites, Worksrcdir   string
	Epoch                     int
	Version                   string
	Revision                  int
	Tag, Distfiles, Checksums string
}

// BuildTargets excludes metadata-only parents from execution, not from scope.
func (s *ReleaseScope) BuildTargets() []Target {
	if s == nil {
		return nil
	}
	var targets []Target
	for _, m := range s.Affected {
		if !m.MetadataOnly {
			target := m.Target
			target.Variants = maps.Clone(target.Variants)
			targets = append(targets, target)
		}
	}
	return targets
}

// Valid checks that a release scope has unique, source-bound members and build work.
// CoverageIntent says which members of a shared release a job's verification
// must build.
type CoverageIntent string

const (
	// CoverageInitiating builds the job's initiating target alone.
	CoverageInitiating CoverageIntent = "initiating"
	// CoverageAll builds every buildable member of the release.
	CoverageAll CoverageIntent = "all"
)

// ErrCoverage reports a coverage intent the release scope cannot satisfy.
var ErrCoverage = errors.New("record: verification coverage cannot be satisfied")

// RequiredTargets lists the members verification must build: every buildable
// member for CoverageAll, otherwise the initiating target alone, which must
// itself be a buildable member. An initiating target that builds nothing in
// this scope is an error, never a silent widening to every member.
func (s *ReleaseScope) RequiredTargets(intent CoverageIntent, initiating Target) ([]Target, error) {
	if s == nil {
		if intent == CoverageInitiating && initiating.Name == "" {
			return nil, fmt.Errorf("%w: no initiating target", ErrCoverage)
		}
		return []Target{initiating}, nil
	}
	buildable := s.BuildTargets()
	switch intent {
	case CoverageAll:
		return buildable, nil
	case CoverageInitiating:
		for _, target := range buildable {
			if CompareTargets(target, initiating) == 0 {
				return []Target{target}, nil
			}
		}
		return nil, fmt.Errorf("%w: initiating target %s builds nothing in this release", ErrCoverage, initiating.Name)
	}
	return nil, fmt.Errorf("%w: unknown intent %q", ErrCoverage, intent)
}

func (s *ReleaseScope) Valid() bool {
	if s == nil {
		return true
	}
	if s.Input.Portfile == "" || len(s.Affected) == 0 || len(s.BuildTargets()) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, group := range [][]ReleaseMember{s.Affected, s.Protected} {
		for _, m := range group {
			if m.Target.Name == "" || m.Target.Portfile != s.Input.Portfile || seen[m.Target.Name] {
				return false
			}
			seen[m.Target.Name] = true
		}
	}
	return true
}

// SameMembership prevents corrections and branch adoption from dropping targets
// or changing the protected source identities accepted by the original bump.
func (s *ReleaseScope) SameMembership(next *ReleaseScope) bool {
	if s == nil || next == nil {
		return s == nil && next == nil
	}
	if !s.Valid() || !next.Valid() || s.Input != next.Input || len(s.Affected) != len(next.Affected) || len(s.Protected) != len(next.Protected) {
		return false
	}
	for i, group := range [][]ReleaseMember{s.Affected, s.Protected} {
		other := next.Affected
		if i == 1 {
			other = next.Protected
		}
		for _, m := range group {
			found := false
			for _, n := range other {
				if CompareTargets(m.Target, n.Target) == 0 && m.MetadataOnly == n.MetadataOnly && m.Before == n.Before && (i == 0 || m.After == n.After) {
					found = true
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}
