package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/cli"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestHelpGroupsCommandsInContributionOrder(t *testing.T) {
	t.Parallel()
	root, err := cli.NewRoot(app.Config{DBPath: t.TempDir() + "/state.db"})
	require.NoError(t, err)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--help"})
	require.NoError(t, root.Execute())
	help := out.String()

	expected := []string{
		"Get started:", "setup", "auth",
		"Investigate ports:", "outdated", "assess",
		"Prepare an update:", "bump", "bump-revision", "checksums",
		"Revise your update:", "amend", "rebase", "reassociate",
		"Verify and publish:", "verify", "publish",
		"Watch and manage jobs:", "status", "console", "wait", "cancel", "serve", "sync", "abandon",
		"Housekeeping:", "gc", "db",
		"Planned, not implemented yet:", "review",
	}
	position := 0
	for _, token := range expected {
		index := strings.Index(help[position:], "\n"+token)
		if index < 0 {
			index = strings.Index(help[position:], "\n  "+token+" ")
		}
		require.GreaterOrEqual(t, index, 0, "expected %q after position %d in help:\n%s", token, position, help)
		position += index + 1
	}
	ungrouped := map[string]bool{"help": true, "completion": true}
	for _, command := range root.Commands() {
		if ungrouped[command.Name()] {
			require.Empty(t, command.GroupID, "%s should stay ungrouped", command.Name())
			continue
		}
		require.NotEmpty(t, command.GroupID, "%s has no help group", command.Name())
	}
}

func TestRunCommandsSectionTheirFlags(t *testing.T) {
	t.Parallel()
	root, err := cli.NewRoot(app.Config{DBPath: t.TempDir() + "/state.db"})
	require.NoError(t, err)
	sectioned := map[string]bool{"bump": true, "bump-revision": true, "checksums": true, "amend": true, "rebase": true, "verify": true, "publish": true}
	for _, command := range root.Commands() {
		command.LocalFlags().VisitAll(func(flag *pflag.Flag) {
			_, has := flag.Annotations["dockhand.section"]
			if sectioned[command.Name()] {
				require.True(t, has || flag.Name == "help", "%s --%s has no section", command.Name(), flag.Name)
			} else {
				require.False(t, has, "%s --%s is sectioned on an unsectioned command", command.Name(), flag.Name)
			}
		})
	}
	help := func(args ...string) string {
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetArgs(append(args, "--help"))
		require.NoError(t, root.Execute())
		return out.String()
	}
	bump := help("bump")
	position := 0
	for _, heading := range []string{"\n\nSelection flags:\n", "\n\nChange flags:\n", "\n\nBuild flags:\n", "\n\nGitHub flags:\n", "\n\nRun flags:\n", "\n\nOther flags:\n  -h, --help", "\n\nGlobal path flags:\n", "\n\nGlobal output flags:\n"} {
		index := strings.Index(bump[position:], heading)
		require.GreaterOrEqual(t, index, 0, "expected %q in order in bump help:\n%s", heading, bump)
		position += index + len(heading)
	}
	require.NotContains(t, bump, "\nFlags:\n")
	require.NotContains(t, bump, "--capacity", "capacity is setup's, not a run's")
	status := help("status")
	require.Contains(t, status, "[flags]\n\nFlags:\n", "an unsectioned command's own flags print whole, a blank line after the usage line")
	for _, heading := range []string{"Selection flags:", "Build flags:", "Run flags:", "Other flags:"} {
		require.NotContains(t, status, heading)
	}
	require.Contains(t, status, "\n\nGlobal path flags:\n")
	top := help()
	require.Contains(t, top, "\nPath flags:\n")
	require.Contains(t, top, "\nOutput flags:\n")
	require.Contains(t, top, "\nOther flags:\n")
	require.Contains(t, help("setup"), "--capacity")
}
