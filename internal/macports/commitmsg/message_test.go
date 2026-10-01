package commitmsg_test

import (
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"github.com/stretchr/testify/require"
)

func TestSubjectSuppliesThePortNameOnce(t *testing.T) {
	t.Parallel()
	subject, err := commitmsg.Subject("py-foo", " revbump for simdutf update ")
	require.NoError(t, err)
	require.Equal(t, "py-foo: revbump for simdutf update", subject)
	_, err = commitmsg.Subject("py-foo", "py-foo: revbump")
	require.ErrorContains(t, err, "already begins with")
	_, err = commitmsg.Subject("py-foo", " ")
	require.ErrorContains(t, err, "one nonempty line")
	_, err = commitmsg.Subject("py-foo", "two\nlines")
	require.ErrorContains(t, err, "one nonempty line")
}

// Dockhand's attribution is known in its current form and in the forms
// earlier builds wrote, so tidy and the pull-request body find it once.
// Another tool's trailer is the person's own.
func TestIsAttributionKnowsEveryFormDockhandWrote(t *testing.T) {
	t.Parallel()
	for _, line := range []string{commitmsg.GeneratedBy(), "  " + commitmsg.GeneratedBy(), "Assisted-By: Dockhand v0.1.0", "Generated-by: dockhand"} {
		require.True(t, commitmsg.IsAttribution(line), line)
	}
	for _, line := range []string{"Assisted-By: Claude Code", "Closes: https://trac.macports.org/ticket/74379", "fixture: update to 2"} {
		require.False(t, commitmsg.IsAttribution(line), line)
	}
}

// A commit is dockhand's when its message carries the attribution line,
// in any form dockhand wrote.
func TestACommitIsDockhandsByItsAttribution(t *testing.T) {
	t.Parallel()
	require.True(t, commitmsg.Attributed("jq: update to 1.8.1\n\n"+commitmsg.GeneratedBy()+"\n"))
	require.True(t, commitmsg.Attributed("jq: update to 1.8.1\n\nAssisted-By: Dockhand v0.1.0"))
	require.False(t, commitmsg.Attributed("jq: update to 1.8.1\n\nAssisted-By: Claude Code\n"))
}

// A commit whose Generated-By names a build of uncommitted source is
// known, as submit shows it; one naming a committed build, or none, isn't.
func TestAModifiedBuildIsKnownFromItsTrailer(t *testing.T) {
	t.Parallel()
	dirty := "hugo: update to 0.167.0\n\nGenerated-By: Dockhand v0.0.0-20260924.0.0.20260928140136-2601fff7d884+dirty (https://github.com/herbygillot/dockhand)\n"
	clean := "hugo: update to 0.167.0\n\nGenerated-By: Dockhand v0.0.0-20260924.0.0.20260928175309-2bbcfdb76480 (https://github.com/herbygillot/dockhand)\n"
	require.True(t, commitmsg.ModifiedBuild(dirty))
	require.False(t, commitmsg.ModifiedBuild(clean))
	require.False(t, commitmsg.ModifiedBuild("hugo: update to 0.167.0\n"))
	require.False(t, commitmsg.ModifiedBuild("hugo: update to 0.167.0\n\nSee: https://example.org/v0.0.0+dirty\n"), "only dockhand's trailer names its build")
}

// A commit can stand for one tidy would write where they say the same,
// but perhaps for the attribution line, which names the build that wrote
// each where both carry it; not where only one does, nor where the one
// written names a build of uncommitted source, which tidying again is
// meant to replace.
func TestACommitThatSaysTheSameIsUnchanged(t *testing.T) {
	message := "jq: update to 1.8.1\n\nGenerated-By: Dockhand v3.0.0 (https://github.com/herbygillot/dockhand)\n"
	for _, test := range []struct {
		had  string
		same bool
	}{
		{message, true},
		{strings.TrimSuffix(message, "\n"), true},
		{"jq: update to 1.8.1\n\nGenerated-By: Dockhand v2.9.0 (https://github.com/herbygillot/dockhand)\n", true},
		{"jq: update to 1.8.1\n", false},
		{"jq: update to 1.8.2\n\nGenerated-By: Dockhand v3.0.0 (https://github.com/herbygillot/dockhand)\n", false},
		{"jq: update to 1.8.1\n\nGenerated-By: Dockhand devel+1a2b3c4d5e6f+dirty (https://github.com/herbygillot/dockhand)\n", false},
	} {
		require.Equal(t, test.same, commitmsg.Unchanged(test.had, message), test.had)
	}
}
