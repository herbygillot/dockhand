package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// update --closes names the Trac ticket the update closes: the branch keeps
// it, each once, and tidy writes it as the update's commit's Closes:
// trailer with its URL, which a later tidy keeps (the docs dig's I41).
func TestAnUpdateClosesTheTicketItNames(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq", Closes: []string{"71234"}})
	require.NoError(t, err)
	var kept model.Branch
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		var err error
		kept, err = r.Branch(branch.ID)
		return err
	}))
	require.Equal(t, []string{"71234"}, kept.Closes)
	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: kept})
	require.NoError(t, err)
	require.Len(t, plan.Groups, 1)
	require.Contains(t, plan.Groups[0].Message, "\n\nCloses: https://trac.macports.org/ticket/71234\n")
	_, err = e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	committed := testsupport.Git(t, branch.Worktree, "log", "-1", "--format=%B")
	require.Contains(t, committed, "Closes: https://trac.macports.org/ticket/71234")
	require.Equal(t, 1, strings.Count(committed, "Closes:"), "once")
}

// A Portfile's comment that reads as a maintainer's instruction is quoted
// with its place, as update, status, and serve say it (the docs dig's I7).
func TestAPortfilesInstructionIsQuotedWithItsPlace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, root, map[string]string{"sysutils/osxfuse/Portfile": "name osxfuse\n# Do not update past 3.8.3.\nversion 3.8.3\n"})
	require.Equal(t, []string{"sysutils/osxfuse/Portfile:2: Do not update past 3.8.3."}, instructionLines(root, "sysutils/osxfuse", "osxfuse"))
	require.Empty(t, instructionLines(root, "sysutils/gone", "gone"))
}
