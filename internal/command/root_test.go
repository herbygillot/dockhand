package command

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/command/settings"
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

// Branch hints are kept in the spelling each command accepts. Walk the
// command paths and flags in the real Cobra tree so a hint can't drift away
// from its command's registered surface.
func TestBranchHintsNameFlagsTheirCommandsAccept(t *testing.T) {
	t.Parallel()
	root := newRoot(&settings.Settings{}, term.Streams{})
	hints := []struct {
		command       string
		branchFlagged bool
	}{
		{"dockhand adopt dockhand/theirs", false},
		{"dockhand archive -b jq-update", true},
		{"dockhand archive --undo -b jq-update", true},
		{"dockhand archive --discard -b jq-update", true},
		{"dockhand clean -b jq-update", true},
		{"dockhand clean --delete -b jq-update", true},
		{"dockhand edit jq -b jq-update", true},
		{"dockhand undo tidy-3", false},
		{"dockhand check -b jq-update", true},
		{"dockhand tidy --branch jq-update", true},
	}
	for _, hint := range hints {
		words := strings.Fields(hint.command)
		require.Equal(t, "dockhand", words[0])
		cmd := root
		for i := 1; i < len(words); i++ {
			word := strings.Trim(words[i], ",.;")
			if strings.HasPrefix(word, "-") {
				name := strings.TrimLeft(word, "-")
				flag := cmd.Flags().Lookup(name)
				if len(word) == 2 {
					flag = cmd.Flags().ShorthandLookup(name)
				}
				if flag == nil {
					flag = cmd.InheritedFlags().Lookup(name)
					if len(word) == 2 {
						flag = cmd.InheritedFlags().ShorthandLookup(name)
					}
				}
				require.NotNil(t, flag, "%q uses %s, which %s does not accept", hint.command, word, cmd.CommandPath())
				continue
			}
			if next := commandChild(cmd, word); next != nil {
				cmd = next
			}
		}
		if hint.branchFlagged {
			words := strings.Fields(hint.command)
			require.True(t, slices.Contains(words, "-b") || slices.Contains(words, "--branch"), "%q should name its branch with a selector", hint.command)
		}
	}
}

func commandChild(parent *cobra.Command, name string) *cobra.Command {
	for _, child := range parent.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
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
	for _, flag := range []string{"--version", "-V"} {
		var out bytes.Buffer
		err := cmdtest.RunFor(t, t.Context(), []string{flag}, term.Streams{In: strings.NewReader(""), Out: &out, Err: &out})
		require.NoError(t, err, flag)
		require.True(t, strings.HasPrefix(out.String(), "dockhand "), "%s: %s", flag, out.String())
	}
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
