package engine

import (
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// A selector names a branch by its name, the start of its name, the one
// port only it changes, its pull request, or a check of it (the
// command-line UX review, §1); one more than one branch answers to is
// refused with them, never guessed.
func TestASelectorNamesOneBranchOrSaysWhichItCouldMean(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	jq := committedUpdate(t, e)
	other, err := e.Start(t.Context(), StartRequest{Name: "jq-tools-9x1z"})
	require.NoError(t, err)
	select_ := func(selector string) (model.Branch, error) {
		t.Helper()
		return e.Select(t.Context(), selector)
	}
	for _, selector := range []string{jq.ShortName(), jq.Name, "jq"} {
		found, err := select_(selector)
		require.NoError(t, err, selector)
		require.Equal(t, jq.ID, found.ID, selector)
	}
	found, err := select_("jq-t")
	require.NoError(t, err)
	require.Equal(t, other.ID, found.ID, "the one name starting so")

	checked(t, e, jq, model.OutcomePassed, model.OutcomePassed)
	runs, err := e.Runs(t.Context(), store.RunFilter{Limit: 1})
	require.NoError(t, err)
	found, err = select_("check-" + strconv.Itoa(runs[0].Number))
	require.NoError(t, err)
	require.Equal(t, jq.ID, found.ID, "a check names its branch")

	jq.PullRequest = &model.PullRequest{Repository: UpstreamRepository, Number: 34901, Head: "ada/macports-ports:" + jq.Name}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.UpdateBranch(jq) }))
	found, err = select_("#34901")
	require.NoError(t, err)
	require.Equal(t, jq.ID, found.ID)
	_, err = select_("#4711")
	require.ErrorIs(t, err, ErrNoBranch)
	require.ErrorContains(t, err, "dockhand adopt --pr 4711 tracks it")

	_, err = select_("libharbor")
	require.ErrorIs(t, err, ErrNoBranch)

	// Two branches changing jq: its name is no longer enough.
	_, err = e.Edit(t.Context(), other, "jq")
	require.NoError(t, err)
	write(t, other.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.9\n"})
	_, err = select_("jq")
	var ambiguous *AmbiguousError
	require.True(t, errors.As(err, &ambiguous), "%v", err)
	require.ErrorIs(t, err, ErrAmbiguous)
	require.Len(t, ambiguous.Branches, 2)
	require.ErrorContains(t, err, "jq names more than one branch: ")
}
