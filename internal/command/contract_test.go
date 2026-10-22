package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// The shared-flag contract (the command UX review's §9, part 5's item 6),
// over the command tree: --json on every command, --plan and --yes apart
// wherever both are, an edit's --plan alike with no branch to plan in, and
// a question without a terminal refused alike. A check run here keeping
// to its provider's capacity is check's test
// (TestAForegroundCheckKeepsToTheProvidersCapacity).

// noJSON are the commands with no --json, each with why.
var noJSON = map[string]string{
	"open":         "opens a page in the browser, with nothing to report",
	"serve":        "a long-running log, read a line an event",
	"shell":        "prints a shell's completion script",
	"setup github": "a sign-in, asked on a terminal",
	"setup tart":   "makes an image, its provisioner's progress the output",
}

// commands are the command tree's runnable commands, by their path below
// the root, as "setup tart".
func commands(t *testing.T) map[string]*cobra.Command {
	t.Helper()
	cmdtest.NewWorld(t)
	found := map[string]*cobra.Command{}
	var walk func(*cobra.Command, string)
	walk = func(c *cobra.Command, prefix string) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			name := strings.TrimSpace(prefix + " " + sub.Name())
			if sub.Runnable() {
				found[name] = sub
			}
			walk(sub, name)
		}
	}
	walk(newRoot(cmdtest.TestSettings(t), term.Streams{}), "")
	return found
}

// Every command reports with --json but those that say why not, and a
// command with --plan and --yes takes one or the other.
func TestTheSharedFlagsMeanOneThing(t *testing.T) {
	t.Parallel()
	for name, cmd := range commands(t) {
		if why, exempt := noJSON[name]; exempt {
			require.NotEmpty(t, why)
			require.NotContains(t, cmd.Annotations, "dockhand.json", "%s says why it has no --json, and has one", name)
			continue
		}
		require.Contains(t, cmd.Annotations, "dockhand.json", "%s has no --json; give it one, or say why in noJSON", name)
		if cmd.Flags().Lookup("plan") != nil && cmd.Flags().Lookup("yes") != nil {
			groups := cmd.Flags().Lookup("plan").Annotations["cobra_annotation_mutually_exclusive"]
			require.Contains(t, strings.Join(groups, ";"), "plan yes", "%s takes --plan and --yes together", name)
		}
	}
	for name := range noJSON {
		require.Contains(t, commands(t), name, "noJSON names a command there isn't")
	}
}

// An edit's --plan with no branch to plan in plans on master as fetched
// now, and starts nothing, with or without --new: update's did, and
// revbump's and checksums' refused (field testing, 2026-10-02).
func TestAnEditsPlanWithNoBranchPlansOnMaster(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)
	for _, args := range [][]string{
		{"update", "jq", "--plan"}, {"update", "jq", "--plan", "--new"},
		{"checksums", "jq", "--plan"}, {"checksums", "jq", "--plan", "--new"},
		{"revbump", "jq", "--plan", "--subject", "rebuild"}, {"revbump", "jq", "--plan", "--new", "jq-rebuild", "--subject", "rebuild"},
	} {
		out, _, err := cmdtest.Dockhand(t, args...)
		require.NoError(t, err, args)
		require.Contains(t, out, "Planned on master ", args)
		require.Empty(t, testsupport.Git(t, w.Clone, "branch", "--list", "dockhand/*"), "%v started a branch", args)
		entries, _ := os.ReadDir(filepath.Join(w.Home, "Source", "macports-branches"))
		require.Empty(t, entries, "%v made a worktree", args)
	}
}

// A question without a terminal, and without --yes, is refused alike:
// exit 1, saying what --yes would do (Design v3 §12; field testing found
// submit's exit 1 and clean's 0).
func TestAQuestionWithoutATerminalIsRefusedAlike(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithGitHub(t, w)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-update"))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "submit", "--no-check")
	require.Equal(t, term.ExitFailure, term.ExitCode(err))
	require.EqualError(t, err, "nothing was submitted: without a terminal, --yes submits exactly what is shown")
}
