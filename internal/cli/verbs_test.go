package cli

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow/view"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestTableVerbsBuildCommandsTheTreeAccepts(t *testing.T) {
	t.Parallel()
	tracked := view.Contribution{Port: "jq", ChangeID: "change_jq"}
	standalone := view.Contribution{Port: "deno", Active: &view.ActiveJob{JobID: "job_2", Action: record.Verify}}
	args, problem := verbArgs("cancel", standalone)
	require.Empty(t, problem)
	require.Equal(t, []string{"cancel", "--job", "job_2"}, args)
	args, problem = verbArgs("verify", standalone)
	require.Empty(t, problem)
	require.Equal(t, []string{"verify", "deno", "--detach"}, args)
	args, problem = verbArgs("bump", tracked)
	require.Empty(t, problem)
	require.Equal(t, []string{"bump", "jq", "--detach"}, args, "a bump continues the port's open contribution or starts afresh")
	args, problem = verbArgs("publish", tracked)
	require.Empty(t, problem)
	require.Equal(t, []string{"publish", "--change", "change_jq", "--detach"}, args)
	_, problem = verbArgs("publish", standalone)
	require.Contains(t, problem, "not a tracked contribution")
	standalone.Active = nil
	_, problem = verbArgs("cancel", standalone)
	require.Equal(t, "nothing is pending", problem)
	require.True(t, verbConfirms("publish"))
	require.False(t, verbConfirms("refresh"))

	// Every verb and flag the table spells must exist in the command tree.
	root, _, err := newRoot(app.Config{}, nil)
	require.NoError(t, err)
	for _, verb := range []string{"bump", "verify", "publish", "cancel", "abandon", "refresh"} {
		for _, row := range []view.Contribution{tracked, standalone} {
			args, problem := verbArgs(verb, row)
			if problem != "" {
				continue
			}
			command, _, err := root.Find(args[:1])
			require.NoError(t, err, verb)
			require.Equal(t, verb, command.Name())
			for _, arg := range args[1:] {
				if len(arg) > 2 && arg[:2] == "--" {
					require.NotNil(t, lookupFlag(command, arg[2:]), "%s takes %s", verb, arg)
				}
			}
		}
	}
}

func lookupFlag(command *cobra.Command, name string) any {
	if flag := command.Flags().Lookup(name); flag != nil {
		return flag
	}
	if flag := command.InheritedFlags().Lookup(name); flag != nil {
		return flag
	}
	return nil
}

// Every verb the table can retry with is a command that takes a port.
func TestRetryVerbsAreCommandsThatTakeAPort(t *testing.T) {
	t.Parallel()
	root, _, err := newRoot(app.Config{}, nil)
	require.NoError(t, err)
	tracked := view.Contribution{Port: "jq", ChangeID: "change_jq"}
	for _, verb := range []string{"bump", "bump-revision", "refresh-checksums", "verify", "publish"} {
		args, problem := verbArgs(verb, tracked)
		require.Empty(t, problem, verb)
		command, _, err := root.Find(args[:1])
		require.NoError(t, err, verb)
		require.Equal(t, verb, command.Name())
		if prepares(verb) {
			require.Equal(t, []string{verb, "jq", "--detach"}, args, "a preparing retry names the port, which continues its contribution")
			require.NoError(t, command.Args(command, []string{"jq"}), "%s accepts a bare port", verb)
		}
		require.True(t, verbConfirms(verb), "%s costs minutes or pushes, so it asks first", verb)
	}
}
