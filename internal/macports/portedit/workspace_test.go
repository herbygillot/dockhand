package portedit

import (
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestSourceInputPathsFollowTheSelectedTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws := adopt(t, root)
	input := &sourceInput{session: &session{ws: ws}, target: record.Target{Portfile: "devel/fixture/Portfile"}}
	require.Equal(t, filepath.Join(ws.Root(), "devel", "fixture", "Portfile"), input.portfile())
	require.Equal(t, filepath.Join(ws.Root(), "devel", "fixture"), input.portdir())
	other := input.forMember(record.Target{Portfile: "devel/sibling/Portfile"})
	require.Equal(t, filepath.Join(ws.Root(), "devel", "sibling"), other.portdir(), "another member's view resolves its own paths in the same session")
	require.Same(t, input.session, other.session)
}

func TestCommitEditRecordsFilesAndFidelityBeforeJudging(t *testing.T) {
	t.Parallel()
	input := &sourceInput{session: &session{}, target: record.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"}}
	request := Request{Subject: "because"}
	edit := portfile.Edit{Path: "devel/fixture/Portfile", After: []byte("new")}
	var result Result
	require.NoError(t, result.commitEdit(input, request, edit, Fidelity{ExpectedChanges: []string{"fixture.revision +1"}}, "revbump"))
	require.Equal(t, []portfile.Edit{edit}, result.Files)
	require.Len(t, result.Fidelity, 1)
	require.Equal(t, []CommitIntent{{Subject: "fixture: because", Paths: []string{"devel/fixture/Portfile"}}}, result.Commits)

	var failed Result
	err := failed.commitEdit(input, request, edit, Fidelity{UnexpectedChanges: []string{"sibling.version changed"}}, "revbump")
	require.ErrorIs(t, err, ErrFidelity)
	require.Equal(t, []portfile.Edit{edit}, failed.Files, "the attempted edit is reported")
	require.Len(t, failed.Fidelity, 1)
	require.Nil(t, failed.Commits, "no commit is intended")
}
