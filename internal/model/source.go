package model

import (
	"strings"
	"time"
)

// GitSource is a Git-fetched port's source as a check expects its build to
// fetch it (the assessment design's source, A). As declared, it is git.url
// and git.branch as MacPorts evaluates them where the port builds; as
// observed, the commit git.branch named when dockhand resolved it from the
// repository's refs, and when. A tag binds nothing, as an archive's
// checksums do, so what a build fetched is a fact of its own, which its
// result keeps (TargetInputs.Fetched), and which a resolution made later
// never stands in for.
type GitSource struct {
	URL string
	// Ref is git.branch: a tag, a branch, or a commit, which MacPorts
	// checks out after cloning; empty for the repository's default branch.
	Ref string `json:",omitempty"`
	// Commit is what Ref named at ResolvedAt: Ref itself where it is a
	// whole commit.
	Commit ObjectID `json:",omitempty"`
	// Abbreviation is set instead where Ref is no ref of the repository but
	// an abbreviated commit, which only a clone expands: the commit a build
	// fetches begins with it.
	Abbreviation string `json:",omitempty"`
	// ResolvedAt is when the repository's refs were read.
	ResolvedAt time.Time `json:",omitzero"`
	// Unresolved says why neither is known: the repository couldn't be
	// read, or none of its refs is Ref.
	Unresolved string `json:",omitempty"`
	// Missing is an Unresolved repository that was read, and has no ref
	// Ref names, which a day may change, as a tag not yet pushed; one not
	// Missing couldn't be read, which another try may meet at once (C8-5).
	Missing bool `json:",omitempty"`
}

// Expected is what a build of the source must check out: the commit, or
// the abbreviation the commit begins with; empty where Ref couldn't be
// resolved, and nothing is expected.
func (s GitSource) Expected() string {
	if s.Commit != "" {
		return string(s.Commit)
	}
	return s.Abbreviation
}

// BuiltBy reports whether a build that checked out fetched built this
// source: the commit Ref named when it was resolved. No build is
// established to have built a source that couldn't be resolved, nor is a
// build that didn't say what it fetched.
func (s GitSource) BuiltBy(fetched ObjectID) bool {
	switch {
	case fetched == "":
		return false
	case s.Commit != "":
		return fetched == s.Commit
	case s.Abbreviation != "":
		return strings.HasPrefix(string(fetched), s.Abbreviation)
	}
	return false
}

// Same reports whether two resolutions name one source: the same
// repository and ref, resolved to the same commit; when each was read
// doesn't matter.
func (s GitSource) Same(other GitSource) bool {
	return s.URL == other.URL && s.Ref == other.Ref && s.Expected() == other.Expected()
}

// Moved reports whether a build that checked out fetched built another
// source than this one: Ref named another commit when the build fetched it
// than when it was resolved, as a tag moved in between does. What isn't
// known on either side hasn't moved; it is only not established (BuiltBy).
func (s GitSource) Moved(fetched ObjectID) bool {
	return s.Expected() != "" && fetched != "" && !s.BuiltBy(fetched)
}

// SourceStanding is what one reading of a source says of another
// (docs/designs/source-comparison.md): the one judgment joining, coverage,
// evidence, reuse, a run's verdict and submit read, each with its own
// policy for what it isn't told.
type SourceStanding int

const (
	// SourceUnknown is one repository and ref, one side naming a commit and
	// the other none, or a whole commit against an abbreviation.
	SourceUnknown SourceStanding = iota
	// SourceUnresolved is one repository and ref, neither side naming a
	// commit.
	SourceUnresolved
	// SourceSame is one repository and ref and one commit, or neither side
	// fetching this way.
	SourceSame
	// SourceMoved is one repository and ref and two commits.
	SourceMoved
	// SourceOther is another repository or ref, or only one side fetching
	// this way. An older release's tag and a newer one's are other, never
	// moved.
	SourceOther
)

// SourceAgainst judges two plans' resolutions of a target's source; nil is
// a target a plan doesn't fetch this way. Two abbreviations under one ref
// are that ref, so the same; a whole commit and an abbreviation say
// nothing of each other, even where one begins with the other.
func SourceAgainst(have, want *GitSource) SourceStanding {
	switch {
	case have == nil && want == nil:
		return SourceSame
	case have == nil || want == nil:
		return SourceOther
	case have.URL != want.URL || have.Ref != want.Ref:
		return SourceOther
	case have.Commit != "" && want.Commit != "":
		if have.Commit == want.Commit {
			return SourceSame
		}
		return SourceMoved
	case have.Commit == "" && want.Commit == "" && have.Abbreviation != "" && want.Abbreviation != "":
		return SourceSame
	case have.Expected() == "" && want.Expected() == "":
		return SourceUnresolved
	}
	return SourceUnknown
}

// AgainstFetched judges a resolution and the whole commit a build says it
// fetched, empty where it said none. It never answers other.
func (s GitSource) AgainstFetched(fetched ObjectID) SourceStanding {
	switch {
	case s.Expected() == "" && fetched == "":
		return SourceUnresolved
	case s.Expected() == "" || fetched == "":
		return SourceUnknown
	case s.Commit != "" && fetched == s.Commit, s.Commit == "" && strings.HasPrefix(string(fetched), s.Abbreviation):
		return SourceSame
	}
	return SourceMoved
}

// problem is what's wrong with a stored source, or nothing: it names its
// repository, and it is resolved, to a commit or an abbreviation, or says
// why not.
func (s GitSource) problem() string {
	switch {
	case s.URL == "":
		return "names no repository"
	case s.Commit != "" && s.Abbreviation != "":
		return "is both a commit and an abbreviation"
	case s.Expected() != "" && s.Unresolved != "":
		return "is resolved and says it isn't"
	case s.Expected() == "" && s.Unresolved == "":
		return "isn't resolved and says not why"
	case s.ResolvedAt.IsZero():
		return "was resolved at no time"
	}
	return ""
}
