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
	require.Contains(t, out.String(), "run dockhand setup")
	require.Contains(t, out.String(), "dockhand usage prints the guide")
	// The one command to a pull request comes first, and the steps by
	// hand under it (the cold-room look of 2026-11-11).
	require.Less(t, strings.Index(out.String(), "dockhand bump <port>"), strings.Index(out.String(), "dockhand update <port>"))
	require.NotContains(t, out.String(), "update <port> --new")
	require.NotContains(t, out.String(), "v2-final")
	require.True(t, strings.HasSuffix(out.String(), "for more information about a command.\n\nReport problems at "+buildinfo.ProjectURL+"/issues\n"), "the main help's foot names the issues page: %s", out.String())
}

// The main help ends with the issues page, after Cobra's line on each
// command's help; a subcommand's help doesn't carry it (the person's
// "Have Dockhand mention its repo issues page in its help output.").
func TestTheMainHelpNamesTheIssuesPage(t *testing.T) {
	t.Parallel()
	out, _, err := cmdtest.Dockhand(t, "--help")
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(out, "Use \"dockhand [command] --help\" for more information about a command.\n\nReport problems at "+buildinfo.ProjectURL+"/issues\n"), out)
	for _, command := range [][]string{{"check", "--help"}, {"setup", "tart", "--help"}} {
		out, _, err := cmdtest.Dockhand(t, command...)
		require.NoError(t, err)
		require.NotContains(t, out, "Report problems at", command)
	}
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
			// The root's are gone on every command (--git, for --tool).
			commands := []string{command}
			if command == "" {
				commands = []string{"status", "update"}
			}
			for _, command := range commands {
				_, _, err := cmdtest.Dockhand(t, command, "--"+flag)
				require.EqualError(t, err, words, "%s --%s", command, flag)
			}
		}
	}
	_, _, err := cmdtest.Dockhand(t, "update", "--no-such-flag")
	require.EqualError(t, err, "unknown flag: --no-such-flag")
}
