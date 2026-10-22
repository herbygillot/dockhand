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

// Dockhand's attribution is known, so tidy and the pull-request body find
// it once. Another tool's trailer is the person's own.
func TestIsAttributionKnowsDockhandsTrailer(t *testing.T) {
	t.Parallel()
	for _, line := range []string{commitmsg.GeneratedBy(), "  " + commitmsg.GeneratedBy()} {
		require.True(t, commitmsg.IsAttribution(line), line)
	}
	for _, line := range []string{"Assisted-By: Claude Code", "Closes: https://trac.macports.org/ticket/74379", "fixture: update to 2"} {
		require.False(t, commitmsg.IsAttribution(line), line)
	}
}

// A commit is dockhand's when its message carries the attribution line.
func TestACommitIsDockhandsByItsAttribution(t *testing.T) {
	t.Parallel()
	require.True(t, commitmsg.Attributed("jq: update to 1.8.1\n\n"+commitmsg.GeneratedBy()+"\n"))
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

// The build a commit's Generated-By names is its tag, which submit asks
// GitHub about; a message without the line names none.
func TestTheBuildAGeneratedByNames(t *testing.T) {
	t.Parallel()
	build, ok := commitmsg.Build("hugo: update to 0.167.0\n\n  Generated-By: Dockhand devel+1a2b3c4d5e6f (https://github.com/herbygillot/dockhand)\n")
	require.True(t, ok)
	require.Equal(t, "devel+1a2b3c4d5e6f", build)
	_, ok = commitmsg.Build("hugo: update to 0.167.0\n")
	require.False(t, ok)
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

// The builds messages' attribution lines name other than this one are
// each named once, by its tag; this build, and a message without the
// line, name none.
func TestTheOtherBuildsCommitsName(t *testing.T) {
	old := "zola: update to 0.22.1\n\nGenerated-By: Dockhand v0.0.0-20260921.2.0.20260923041537-ec1997b8bc53 (https://github.com/herbygillot/dockhand)\n"
	current := "yq: update to 4.54.1\n\n" + commitmsg.GeneratedBy() + "\n"
	require.Equal(t, []string{"v0.0.0-20260921.2.0.20260923041537-ec1997b8bc53"},
		commitmsg.OtherBuilds([]string{old, current, "a hand commit\n", old}))
	require.Empty(t, commitmsg.OtherBuilds([]string{current}))
}

func TestSubjectPorts(t *testing.T) {
	require.Equal(t, []string{"gojq", "jq"}, commitmsg.SubjectPorts("gojq, jq: rebuild"))
	require.Equal(t, []string{"jq"}, commitmsg.SubjectPorts("jq: update to 1.8.1"))
	require.Nil(t, commitmsg.SubjectPorts("Update jq"))
}

// A combined commit's message is its subject, its body, and the trailers
// the commits it combines carried, Closes: before See: before the rest,
// each once, an earlier Generated-By: left out, and this one last where dockhand
// wrote the edit: tidy's composer, here beside its rules (the
// code-organization review's finding 9).
func TestComposeKeepsTheCombinedCommitsTrailers(t *testing.T) {
	t.Parallel()
	combined := []string{
		"jq: update to 1.8.1\n\nSee: https://trac.macports.org/ticket/1\nCloses: https://trac.macports.org/ticket/2\n",
		"jq: fix the build\n\nGenerated-By: Dockhand v0.3.0 (https://github.com/herbygillot/dockhand)\nCloses: https://trac.macports.org/ticket/2\nReported-by: Ada\n",
	}
	require.Equal(t, "jq: update to 1.8.1\n\nThe body.\n\nCloses: https://trac.macports.org/ticket/2\nSee: https://trac.macports.org/ticket/1\nReported-by: Ada\n"+commitmsg.GeneratedBy()+"\n",
		commitmsg.Compose("jq: update to 1.8.1", "The body.", combined, true))
	require.Equal(t, "jq: update to 1.8.1\n", commitmsg.Compose("jq: update to 1.8.1", "", nil, false))
	text, trailers := commitmsg.SplitTrailers("The body.\n\nSee: x\nCloses: y")
	require.Equal(t, "The body.", text)
	require.Equal(t, []string{"See: x", "Closes: y"}, trailers)
	require.Equal(t, "The body.", commitmsg.Body("jq: x\n\nThe body.\n\nSee: z\n"))
}
