package macports

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// SourceMatch is one entry of a port's source set: an archive the base
// fetches beside the one the candidate fetches in its place (matched), one
// only the candidate fetches (added), one only the base did (removed), or
// one that several could correspond to, which isn't guessed (uncertain).
// Before and After name the archives; one is empty where only one side
// has it, and an uncertain entry has one side alone (the architecture
// review's finding 2).
type SourceMatch struct {
	Before, After string
	Status        SourceStatus
	// Basis is what a match rests on, strongest first: the same name, a
	// replacement the editor observed, the same name but for its version,
	// or being the one left on each side.
	Basis SourceBasis
}

// SourceStatus is how an entry of a source set stands.
type SourceStatus string

const (
	SourceMatched   SourceStatus = "matched"
	SourceAdded     SourceStatus = "added"
	SourceRemoved   SourceStatus = "removed"
	SourceUncertain SourceStatus = "uncertain"
)

// SourceBasis is what a match rests on.
type SourceBasis string

const (
	ByName     SourceBasis = "name"
	ByObserved SourceBasis = "observed"
	ByPattern  SourceBasis = "pattern"
	BySole     SourceBasis = "sole"
)

// Identity is what an entry is known by from one version to the next:
// its archive's name with its versions masked, the candidate's where it
// has one, since the name itself carries the version, and a finding about
// jq-1.7.1.tar.gz is about jq-1.8.0.tar.gz's too (D14).
func (m SourceMatch) Identity() string {
	if m.After != "" {
		return sourcePattern(m.After)
	}
	return sourcePattern(m.Before)
}

// Name is the archive an entry is read as: the candidate's, or the base's
// where the candidate has none.
func (m SourceMatch) Name() string {
	if m.After != "" {
		return m.After
	}
	return m.Before
}

// MatchSources is the one correspondence of a port's archives at two
// versions, which update, a revision's assessment, and the archive diff
// read alike, where they had paired by observation, by name then order,
// and by position. An archive both name alike is itself on each side;
// observed are replacements the editor saw, each a base's archive and the
// candidate's in its place; one the same as another but for its versions,
// each unique so on its side, is its replacement; and where one archive is
// left on each side, they correspond. What's left past that, with more
// than one on a side, is uncertain, never paired by order: a reordering
// isn't a replacement. An archive left on one side only is added or
// removed.
func MatchSources(before, after []string, observed [][2]string) []SourceMatch {
	var matches []SourceMatch
	left := [2][]string{slices.Clone(before), slices.Clone(after)}
	take := func(side int, name string) bool {
		i := slices.Index(left[side], name)
		if i < 0 {
			return false
		}
		left[side] = slices.Delete(left[side], i, i+1)
		return true
	}
	for _, name := range before {
		if slices.Contains(left[1], name) && take(0, name) && take(1, name) {
			matches = append(matches, SourceMatch{Before: name, After: name, Status: SourceMatched, Basis: ByName})
		}
	}
	for _, pair := range observed {
		if slices.Contains(left[0], pair[0]) && slices.Contains(left[1], pair[1]) {
			take(0, pair[0])
			take(1, pair[1])
			matches = append(matches, SourceMatch{Before: pair[0], After: pair[1], Status: SourceMatched, Basis: ByObserved})
		}
	}
	unique := func(names []string, pattern string) (string, bool) {
		var found []string
		for _, name := range names {
			if sourcePattern(name) == pattern {
				found = append(found, name)
			}
		}
		if len(found) != 1 {
			return "", false
		}
		return found[0], true
	}
	for _, name := range slices.Clone(left[0]) {
		pattern := sourcePattern(name)
		mine, ok := unique(left[0], pattern)
		if !ok || mine != name {
			continue
		}
		theirs, ok := unique(left[1], pattern)
		if !ok {
			continue
		}
		take(0, name)
		take(1, theirs)
		matches = append(matches, SourceMatch{Before: name, After: theirs, Status: SourceMatched, Basis: ByPattern})
	}
	switch {
	case len(left[0]) == 1 && len(left[1]) == 1:
		matches = append(matches, SourceMatch{Before: left[0][0], After: left[1][0], Status: SourceMatched, Basis: BySole})
	case len(left[0]) > 0 && len(left[1]) > 0:
		for _, name := range left[0] {
			matches = append(matches, SourceMatch{Before: name, Status: SourceUncertain})
		}
		for _, name := range left[1] {
			matches = append(matches, SourceMatch{After: name, Status: SourceUncertain})
		}
	default:
		for _, name := range left[0] {
			matches = append(matches, SourceMatch{Before: name, Status: SourceRemoved})
		}
		for _, name := range left[1] {
			matches = append(matches, SourceMatch{After: name, Status: SourceAdded})
		}
	}
	return matches
}

// versionRun is what a version is spelled with in an archive's name: a
// commit's hash, or a run of digits and the separators between them.
var versionRun = regexp.MustCompile(`[0-9a-f]{7,40}|[0-9]+(?:[._-][0-9]+)*`)

// sourcePattern is an archive's name with its versions masked:
// jq-1.8.0.tar.gz is jq-*.tar.gz. A run of hex letters without a digit is
// a word, not a hash.
func sourcePattern(name string) string {
	return versionRun.ReplaceAllStringFunc(name, func(run string) string {
		if strings.IndexFunc(run, unicode.IsDigit) < 0 {
			return run
		}
		return "*"
	})
}
