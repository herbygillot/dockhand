package workflow_test

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestAdoptTracksOneCommitAboveMasterInOnePortDirectory(t *testing.T) {
	t.Parallel()
	f, _ := manualPublicationFixture(t)
	request := workflow.AdoptRequest{Branch: "candidate", Upstream: f.source.Base, Platform: buildPlatform, DryRun: true}
	dry, err := f.engine.AdoptContribution(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, "devel/fixture/Portfile", dry.Portfile)
	require.Equal(t, "fixture", dry.Change.InitiatingTarget, "the port is inferred from the directory the branch changes")
	require.Contains(t, dry.Detail, "nothing recorded")
	_, err = f.engine.SelectContribution(t.Context(), workflow.ContributionSelector{Branch: "candidate"})
	require.ErrorIs(t, err, state.ErrNotFound, "a dry run records nothing")

	request.DryRun = false
	result, err := f.engine.AdoptContribution(t.Context(), request)
	require.NoError(t, err)
	change, err := f.engine.SelectContribution(t.Context(), workflow.ContributionSelector{Branch: "candidate"})
	require.NoError(t, err)
	require.Equal(t, result.Change.ID, change.ID)
	require.Equal(t, "candidate", change.Branch)
	require.Equal(t, []record.Target{{Name: "fixture", Portfile: "devel/fixture/Portfile"}}, change.Targets)
	require.Equal(t, f.source, result.Revision.Source, "the revision is the branch's commit above its master base")
	require.Contains(t, result.Detail, "verify fixture builds it")
	_, err = f.engine.AdoptContribution(t.Context(), request)
	require.ErrorContains(t, err, "already tracked as the contribution for fixture")
	request.Branch = "no-such-branch"
	_, err = f.engine.AdoptContribution(t.Context(), request)
	require.Error(t, err)
}

func TestAdoptRefusesSeveralCommitsAndSeveralDirectories(t *testing.T) {
	t.Parallel()
	f, _ := manualPublicationFixture(t)
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: f.now()}
	stacked, err := f.repo.WriteCommit(t.Context(), git.Commit{Tree: string(f.source.Tree), Parents: []string{string(f.source.Commit)}, Message: "fixture: second thoughts", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/stacked", Desired: git.RefValue{Exists: true, Object: stacked}}}))
	_, err = f.engine.AdoptContribution(t.Context(), workflow.AdoptRequest{Branch: "stacked", Upstream: f.source.Base, Platform: buildPlatform})
	require.ErrorContains(t, err, "2 commits above master")

	blob, err := f.repo.WriteBlob(t.Context(), []byte("version 3\n"))
	require.NoError(t, err)
	port, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "Portfile", Object: blob, Type: "blob", Mode: 0100644}})
	require.NoError(t, err)
	category, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "fixture", Object: port, Type: "tree", Mode: 040000}, {Name: "other", Object: port, Type: "tree", Mode: 040000}})
	require.NoError(t, err)
	tree, err := f.repo.WriteTree(t.Context(), []git.TreeEntry{{Name: "devel", Object: category, Type: "tree", Mode: 040000}})
	require.NoError(t, err)
	wide, err := f.repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Parents: []string{string(f.source.Base)}, Message: "two ports", Author: sig, Committer: sig})
	require.NoError(t, err)
	require.NoError(t, f.repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/wide", Desired: git.RefValue{Exists: true, Object: wide}}}))
	_, err = f.engine.AdoptContribution(t.Context(), workflow.AdoptRequest{Branch: "wide", Upstream: f.source.Base, Platform: buildPlatform})
	require.ErrorContains(t, err, "one port directory")
}
