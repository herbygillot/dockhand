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
// over the command tree: --json and --plan on every command but those that
// say why not, --plan and --yes apart
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

// noPlan are the commands with no --plan, each with why: what they do is
// what they'd show, they ask before what they change, or, create and
// rebase, a plan of their own is Later's, since each is undone already
// (the roadmap's bump and clean fix, 2026-10-22). Every other command
// takes --plan, so a new one says here why it doesn't.
var noPlan = map[string]string{
	"adopt":        "records a branch that exists, which archive sets aside again",
	"archive":      "sets a branch aside, which archive --undo brings back",
	"cancel":       "stops a check, keeping what it finished",
	"config":       "shows or sets one setting, which it names",
	"diff":         "reads only",
	"edit":         "opens the Portfile in your editor",
	"explain":      "reads only",
	"impact":       "reads only",
	"logs":         "reads only",
	"artifacts":    "reads only",
	"open":         "opens a page",
	"outdated":     "reads only",
	"path":         "says a branch's directory, checking it out again where it's gone",
	"retry":        "queues a check again, as check --plan previews",
	"review":       "shows the review and posts it only when asked",
	"serve":        "runs what's queued; --drain says when it stops",
	"setup":        "says what's set up, and asks before each part it sets up",
	"setup github": "a sign-in, asked on a terminal",
	"setup tart":   "makes an image, saying its size first",
	"shell":        "prints a shell's completion script",
	"start":        "starts a branch, which archive sets aside",
	"status":       "reads only",
	"undo":         "restores a checkpoint, keeping the one it replaces",
	"wait":         "follows a check",
	"create":       "Later: a plan of what it would write; its branch is archived to undo it",
	"rebase":       "Later: a plan of the commits it would replay; its checkpoint undoes it",
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

// Every command takes --plan but those that say why not.
func TestEveryCommandPlansOrSaysWhyNot(t *testing.T) {
	t.Parallel()
	found := commands(t)
	for name, cmd := range found {
		_, exempt := noPlan[name]
		hasPlan := cmd.Flags().Lookup("plan") != nil
		require.True(t, hasPlan != exempt, "%s: --plan %v, and noPlan names it %v; give it --plan, or say why not", name, hasPlan, exempt)
	}
	for name := range noPlan {
		require.Contains(t, found, name, "noPlan names a command there isn't")
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
