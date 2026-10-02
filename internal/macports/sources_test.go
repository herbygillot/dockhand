package macports_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

// A port's archives at two versions correspond one way, for update, a
// revision's assessment, and the archive diff alike: a removal stays
// visible, a reordering isn't a replacement, an observed rename is one,
// and what can't be told apart isn't guessed (the architecture review's
// finding 2).
func TestSourcesMatchByWhatTheyAre(t *testing.T) {
	matched := func(before, after string, basis macports.SourceBasis) macports.SourceMatch {
		return macports.SourceMatch{Before: before, After: after, Status: macports.SourceMatched, Basis: basis}
	}
	for _, test := range []struct {
		name          string
		before, after []string
		observed      [][2]string
		want          []macports.SourceMatch
	}{
		{"one archive, a new version", []string{"jq-1.7.1.tar.gz"}, []string{"jq-1.8.0.tar.gz"}, nil,
			[]macports.SourceMatch{matched("jq-1.7.1.tar.gz", "jq-1.8.0.tar.gz", macports.ByPattern)}},
		{"a supplementary archive removed", []string{"main-1.0.tar.gz", "extra.tar.gz"}, []string{"main-1.1.tar.gz"}, nil,
			[]macports.SourceMatch{matched("main-1.0.tar.gz", "main-1.1.tar.gz", macports.ByPattern), {Before: "extra.tar.gz", Status: macports.SourceRemoved}}},
		{"one added", []string{"main-1.0.tar.gz"}, []string{"main-1.1.tar.gz", "docs-1.1.tar.gz"}, nil,
			[]macports.SourceMatch{matched("main-1.0.tar.gz", "main-1.1.tar.gz", macports.ByPattern), {After: "docs-1.1.tar.gz", Status: macports.SourceAdded}}},
		{"reordered, each by its own", []string{"a-1.0.tar.gz", "b-1.0.tar.gz"}, []string{"b-2.0.tar.gz", "a-2.0.tar.gz"}, nil,
			[]macports.SourceMatch{matched("a-1.0.tar.gz", "a-2.0.tar.gz", macports.ByPattern), matched("b-1.0.tar.gz", "b-2.0.tar.gz", macports.ByPattern)}},
		{"the same name on each side", []string{"a-1.0.tar.gz", "fonts.zip"}, []string{"fonts.zip", "a-2.0.tar.gz"}, nil,
			[]macports.SourceMatch{matched("fonts.zip", "fonts.zip", macports.ByName), matched("a-1.0.tar.gz", "a-2.0.tar.gz", macports.ByPattern)}},
		{"renamed, as the editor observed", []string{"old-name-1.0.tgz", "x-1.0.tgz"}, []string{"x-2.0.tgz", "new-name-2.0.tgz"}, [][2]string{{"old-name-1.0.tgz", "new-name-2.0.tgz"}},
			[]macports.SourceMatch{matched("old-name-1.0.tgz", "new-name-2.0.tgz", macports.ByObserved), matched("x-1.0.tgz", "x-2.0.tgz", macports.ByPattern)}},
		{"renamed, the one left", []string{"old-name-1.0.tgz"}, []string{"new-name-2.0.tgz"}, nil,
			[]macports.SourceMatch{matched("old-name-1.0.tgz", "new-name-2.0.tgz", macports.BySole)}},
		{"a commit's archive", []string{"repo-0123abc4567.tar.gz"}, []string{"repo-89def01234a.tar.gz"}, nil,
			[]macports.SourceMatch{matched("repo-0123abc4567.tar.gz", "repo-89def01234a.tar.gz", macports.ByPattern)}},
		{"renamed past telling", []string{"one.tgz", "two.tgz"}, []string{"uno.tgz", "dos.tgz"}, nil,
			[]macports.SourceMatch{{Before: "one.tgz", Status: macports.SourceUncertain}, {Before: "two.tgz", Status: macports.SourceUncertain},
				{After: "uno.tgz", Status: macports.SourceUncertain}, {After: "dos.tgz", Status: macports.SourceUncertain}}},
		{"a new port", nil, []string{"new-1.0.tar.gz"}, nil,
			[]macports.SourceMatch{{After: "new-1.0.tar.gz", Status: macports.SourceAdded}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, macports.MatchSources(test.before, test.after, test.observed))
		})
	}
}

// An entry is known by its archive's name with the version masked, which
// the next version's archive shares (D14).
func TestASourcesIdentityOutlivesItsVersion(t *testing.T) {
	jq := macports.SourceMatch{Before: "jq-1.7.1.tar.gz", After: "jq-1.8.0.tar.gz", Status: macports.SourceMatched}
	require.Equal(t, "jq-*.tar.gz", jq.Identity())
	require.Equal(t, "jq-1.8.0.tar.gz", jq.Name())
	gone := macports.SourceMatch{Before: "extra-2.tar.gz", Status: macports.SourceRemoved}
	require.Equal(t, "extra-*.tar.gz", gone.Identity())
	require.Equal(t, "facade-*.zip", macports.SourceMatch{After: "facade-3.zip"}.Identity(), "a word of hex letters isn't a hash")
}
