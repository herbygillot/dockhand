package engine

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A branch serve prepared and committed and never queued a check of, as a
// serve stopped before QueueCheck leaves it, is queued as serve starts;
// one with edits not committed is left for the person, and a person's own
// branch, or one with a run, isn't serve's to resume (Codex's review of
// 2ad896d2, W6).
func TestServeResumesABranchItPreparedAndDidntQueue(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	prepared := func(name string, origin model.Origin) model.Branch {
		branch, err := e.Start(t.Context(), StartRequest{Name: name, Origin: origin})
		require.NoError(t, err)
		_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
		require.NoError(t, err)
		plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
		require.NoError(t, err)
		_, err = e.ApplyTidy(t.Context(), plan)
		require.NoError(t, err)
		return branch
	}
	stranded := prepared("jq-stranded", model.OriginServe)
	edited := prepared("jq-edited", model.OriginServe)
	testsupport.WriteFiles(t, edited.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# mine\n"})
	prepared("jq-mine", model.OriginPerson)
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}})
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}

	resumed, err := e.ResumePrepared(t.Context(), Where{Environments: []model.Environment{{Provider: "command"}}}, "", true)
	require.NoError(t, err)
	require.Len(t, resumed, 2, "serve's two, not the person's")
	byName := map[string]Resumed{}
	for _, done := range resumed {
		byName[done.Branch.ShortName()] = done
	}
	require.NotNil(t, byName[stranded.ShortName()].Run, byName[stranded.ShortName()].Problem)
	require.True(t, byName[edited.ShortName()].HandEdits)
	require.Contains(t, byName[edited.ShortName()].Problem, "dockhand ship -b "+edited.ShortName())

	again, err := e.ResumePrepared(t.Context(), Where{Environments: []model.Environment{{Provider: "command"}}}, "", true)
	require.NoError(t, err)
	require.Len(t, again, 1, "one with a run now isn't resumed again")
}

func TestUnattendedShipCommitsMissingFinalNewlineUnchanged(t *testing.T) {
	t.Parallel()
	f := setup(t)
	testsupport.WriteFiles(t, f.upstream, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\nchecksums sha256 0000"})
	testsupport.Git(t, f.upstream, "add", "textproc/jq/Portfile")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "jq: remove final newline")
	e, _ := f.withPreparer(t)

	done := e.ship(t.Context(), shipTarget{Port: "jq", Version: "1.8.1", Name: "jq-update"}, shipOptions{
		Origin: model.OriginServe, To: ShipCommit, Answered: model.AnsweredNobody,
	})
	require.True(t, done.Tidied, done.Problem)
	require.Empty(t, done.Problem)
	require.Empty(t, done.Update.Fixed)
	require.Equal(t, modelineHead+"name jq\nversion 1.8.1\nchecksums sha256 0000", read(t, filepath.Join(done.Branch.Worktree, "textproc/jq/Portfile")))
}

func TestServeRecoveryQueuesUnattendedCommitWithoutFinalNewline(t *testing.T) {
	t.Parallel()
	f := setup(t)
	testsupport.WriteFiles(t, f.upstream, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.7.1\nchecksums sha256 0000"})
	testsupport.Git(t, f.upstream, "add", "textproc/jq/Portfile")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "jq: remove final newline")
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-serve", Origin: model.OriginServe})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq", Unattended: true})
	require.NoError(t, err)
	const expected = modelineHead + "name jq\nversion 1.8.1\nchecksums sha256 0000"
	require.Equal(t, expected, read(t, filepath.Join(branch.Worktree, "textproc/jq/Portfile")), "update leaves the missing final newline alone")
	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.Empty(t, plan.Fixed)
	require.True(t, plan.Groups[0].FromEdits)
	_, err = e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)

	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}})
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	resumed, err := e.ResumePrepared(t.Context(), Where{Environments: []model.Environment{{Provider: "command"}}}, "", true)
	require.NoError(t, err)
	require.Len(t, resumed, 1)
	require.NotNil(t, resumed[0].Run, resumed[0].Problem)
	require.Equal(t, expected, read(t, filepath.Join(branch.Worktree, "textproc/jq/Portfile")))
}
