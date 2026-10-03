package engine

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// The branches changing a port are found by it, and a pull request's
// branch by its number (the command-line UX review's §1, revised); a port
// two branches change says both, and a pull request no branch tracks
// names the adopt that tracks it.
func TestABranchIsFoundByThePortItChangesOrItsPullRequest(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	jq := committedUpdate(t, e)
	found, err := e.PortBranches(t.Context(), "jq")
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Equal(t, jq.ID, found[0].Branch.ID)
	require.False(t, found[0].RevisionOnly, "without a record, its text says it changes jq")
	none, err := e.PortBranches(t.Context(), "libharbor")
	require.NoError(t, err)
	require.Empty(t, none)

	jq.PullRequest = &model.PullRequest{Repository: UpstreamRepository, Number: 34901, Head: "ada/macports-ports:" + jq.Name}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.UpdateBranch(jq) }))
	tracking, err := e.PullRequestBranch(t.Context(), 34901)
	require.NoError(t, err)
	require.Equal(t, jq.ID, tracking.ID)
	_, err = e.PullRequestBranch(t.Context(), 4711)
	require.ErrorIs(t, err, ErrNoBranch)
	require.ErrorContains(t, err, "dockhand adopt --pr 4711 tracks it")

	other, err := e.Start(t.Context(), StartRequest{Name: "jq-again"})
	require.NoError(t, err)
	_, err = e.Edit(t.Context(), other, "jq")
	require.NoError(t, err)
	write(t, other.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.9\n"})
	found, err = e.PortBranches(t.Context(), "jq")
	require.NoError(t, err)
	require.Len(t, found, 2)
	err = &AmbiguousError{Port: "jq", Branches: found}
	require.True(t, errors.Is(err, ErrAmbiguous))
	require.ErrorContains(t, err, "jq is changed in more than one open branch: ")
	require.ErrorContains(t, err, "name one with -b")
}

// A change record that moves only a subport's revision says so, and such
// a branch ranks after one that changes more: `-p jq` means jq's own
// update before a library branch's rebuild of it.
func TestARevisionOnlyChangeIsSaid(t *testing.T) {
	record := model.ChangeRecord{Ports: []model.SubportChange{
		{Port: "jq", Kind: model.SubportChanged, Fields: []model.FieldChange{{Field: "revision", From: "0", To: "1"}}},
		{Port: "jq-devel", Kind: model.SubportChanged, Fields: []model.FieldChange{{Field: "revision", From: "0", To: "1"}, {Field: "version", From: "1", To: "2"}}},
	}}
	require.True(t, record.RevisionOnly("jq"))
	require.False(t, record.RevisionOnly("jq-devel"))
	require.False(t, record.RevisionOnly("libharbor"))
}

// A branch is named for what it does, its short ID added only where the
// name is taken now; what Git refuses in a ref is written -.
func TestABranchIsNamedForWhatItDoes(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	name, err := e.NameFor(t.Context(), "jq", "1.8.1")
	require.NoError(t, err)
	require.Equal(t, "jq-1.8.1", name)
	_, err = e.Start(t.Context(), StartRequest{Name: name})
	require.NoError(t, err)
	again, err := e.NameFor(t.Context(), "jq", "1.8.1")
	require.NoError(t, err)
	require.Regexp(t, `^jq-1\.8\.1-[a-z0-9]{4}$`, again, "taken, so the ID is added")
	odd, err := e.NameFor(t.Context(), "jq", "1.8~rc1:2 ")
	require.NoError(t, err)
	require.Equal(t, "jq-1.8-rc1-2", odd)
	plain, err := e.FreeName(t.Context(), "jq")
	require.NoError(t, err)
	require.Regexp(t, `^jq-[a-z0-9]{4}$`, plain, "nothing more to say: the port and an ID")
}
