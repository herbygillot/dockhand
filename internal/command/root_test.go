package command

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/term"
)

// Outside a ports checkout, dockhand alone says how to begin.
func TestBareDockhandSaysHowToStart(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	err := cmdtest.RunFor(t, t.Context(), nil, term.Streams{In: strings.NewReader(""), Out: &out, Err: &out})
	require.NoError(t, err)
	require.Contains(t, out.String(), "dockhand setup sets up")
	require.Contains(t, out.String(), "dockhand usage prints the guide")
	require.NotContains(t, out.String(), "v2-final")
}

func TestVersionNamesTheBuild(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	err := cmdtest.RunFor(t, t.Context(), []string{"--version"}, term.Streams{In: strings.NewReader(""), Out: &out, Err: &out})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(out.String(), "dockhand "), out.String())
}

// The main help opens with dockhand's logo, as earlier generations' did,
// and the build's version on the line under it.
func TestTheHelpOpensWithTheLogoAndTheVersion(t *testing.T) {
	t.Parallel()
	out, _, err := cmdtest.Dockhand(t, "--help")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(out, logo+" "+buildinfo.Current().String()+"\n\nPrepare, check, and submit changes to MacPorts ports.\n"), out)
}

// A removed flag says what replaced it, or that nothing did, rather than
// "unknown flag" (the docs re-review's item 7, 2026-10-27), and none names
// a flag its command has, which would never be reached.
func TestARemovedFlagSaysWhatReplacedIt(t *testing.T) {
	t.Parallel()
	for command, flags := range removedFlags {
		for flag, words := range flags {
			_, _, err := cmdtest.Dockhand(t, command, "--"+flag)
			require.EqualError(t, err, words, "%s --%s", command, flag)
		}
	}
	_, _, err := cmdtest.Dockhand(t, "update", "--no-such-flag")
	require.EqualError(t, err, "unknown flag: --no-such-flag")
}

// The spend line says GraphQL's points beside the requests, where a
// command's queries cost any.
func TestSpendWordsSayGraphQLPointsBesideRequests(t *testing.T) {
	t.Parallel()
	require.Equal(t, "GitHub's API: 3 requests", spendWords(3, 0))
	require.Equal(t, "GitHub's API: 40 requests, and 12 points of GraphQL's", spendWords(40, 12))
	require.Equal(t, "GitHub's API: 0 requests, and 1 point of GraphQL's", spendWords(0, 1))
}
