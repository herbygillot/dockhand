package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/cli"
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
		"Prepare an update:", "bump", "bump-revision", "refresh-checksums",
		"Revise your update:", "amend", "rebase", "reassociate",
		"Verify and publish:", "verify", "publish",
		"Watch and manage jobs:", "status", "wait", "cancel", "start", "refresh", "abandon",
		"Housekeeping:", "gc", "db",
		"Additional Commands:", "review",
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
	ungrouped := map[string]bool{"help": true, "completion": true, "review": true}
	for _, command := range root.Commands() {
		if ungrouped[command.Name()] {
			require.Empty(t, command.GroupID, "%s should stay ungrouped", command.Name())
			continue
		}
		require.NotEmpty(t, command.GroupID, "%s has no help group", command.Name())
	}
}
