package workflow_test

import (
	"context"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func checkoutFixture(t *testing.T, f *fixture) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", "checkout", "candidate")
	command.Dir = f.repo.Root
	out, err := command.CombinedOutput()
	require.NoError(t, err, "%s", out)
}
func TestWorkingTreeVerificationFreezesInputAcrossEditsAndDriverRestart(t *testing.T) {
	t.Parallel()
	for _, tracked := range []bool{false, true} {
		t.Run(map[bool]string{false: "standalone", true: "tracked"}[tracked], func(t *testing.T) {
			f, ports := bindingFixture(t)
			checkoutFixture(t, f)
			if tracked {
				trackCandidate(t, f)
			}
			file := filepath.Join(f.repo.Root, "devel/fixture/Portfile")
			require.NoError(t, os.WriteFile(file, []byte("version 2\n"), 0600))
			input := bindRequest(f, "working")
			input.Branch = ""
			bound, err := f.engine.BindVerification(t.Context(), input)
			require.NoError(t, err)
			source := bound.Request.Spec.Source
			require.Empty(t, source.Commit)
			require.Equal(t, 1, bound.Request.Spec.Checkout.ModifiedFiles)
			require.Equal(t, "version 2\n", ports.seenBytes)
			require.NoError(t, os.WriteFile(file, []byte("version 3\n"), 0600))
			receipt, err := f.engine.Submit(t.Context(), bound.Request)
			require.NoError(t, err)
			require.NoError(t, f.store.Close())
			reopened, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
			require.NoError(t, err)
			defer reopened.Close()
			f.store = reopened
			f.engine.State = state.Bind(reopened, f.registration)
			status := f.status(t, receipt.JobID)
			require.Equal(t, source, status.Jobs[0].Job.Spec.Source)
			require.Equal(t, bound.Request.Spec.Checkout, status.Jobs[0].Job.Spec.Checkout)
			require.Equal(t, tracked, status.Jobs[0].Job.ChangeID != "")
			retry, err := f.engine.Submit(t.Context(), bound.Request)
			require.NoError(t, err)
			require.Equal(t, receipt, retry)
			f.run(t, receipt.JobID)
			require.Equal(t, source, f.attempt(t, receipt.JobID).Spec.Source)
			committed, err := f.engine.BindVerification(t.Context(), bindRequest(f, "committed"))
			require.NoError(t, err)
			require.NotEmpty(t, committed.Request.Spec.Source.Commit)
			require.Nil(t, committed.Request.Spec.Checkout)
			require.Equal(t, "version 1\n", ports.seenBytes)
		})
	}
}
func TestUntrackedSourceMustBeStagedBeforeWorkingTreeVerification(t *testing.T) {
	t.Parallel()
	f, _ := bindingFixture(t)
	checkoutFixture(t, f)
	file := filepath.Join(f.repo.Root, "devel/fixture/patch.diff")
	require.NoError(t, os.WriteFile(file, []byte("patch"), 0600))
	input := bindRequest(f, "working")
	input.Branch = ""
	_, err := f.engine.BindVerification(t.Context(), input)
	require.ErrorContains(t, err, "git add")
	require.ErrorContains(t, err, "patch.diff")
	status, err := f.engine.Status(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	require.Empty(t, status.Jobs)
	_, err = f.engine.BindVerification(t.Context(), bindRequest(f, "committed"))
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), "git", "add", "devel/fixture/patch.diff")
	command.Dir = f.repo.Root
	out, err := command.CombinedOutput()
	require.NoError(t, err, "%s", out)
	bound, err := f.engine.BindVerification(t.Context(), input)
	require.NoError(t, err)
	require.Equal(t, 1, bound.Request.Spec.Checkout.ModifiedFiles)
}

func TestWorkingBindingOutsideTransactionAndDetachedCheckout(t *testing.T) {
	t.Parallel()
	f, ports := bindingFixture(t)
	checkoutFixture(t, f)
	command := exec.CommandContext(t.Context(), "git", "checkout", "--detach")
	command.Dir = f.repo.Root
	out, err := command.CombinedOutput()
	require.NoError(t, err, "%s", out)
	ports.onEvaluate = func() {
		_, err := f.engine.Status(context.Background(), workflow.Scope{All: true})
		require.NoError(t, err)
	}
	request := bindRequest(f, "detached")
	request.Branch = ""
	bound, err := f.engine.BindVerification(t.Context(), request)
	require.NoError(t, err)
	require.Nil(t, bound.Request.Branch)
	require.Empty(t, bound.Request.Spec.Checkout.Branch)
	require.NotEqual(t, record.Source{}, bound.Request.Spec.Source)
	require.NotEmpty(t, bound.Request.Spec.Source.Commit)
	receipt, err := f.engine.Submit(t.Context(), bound.Request)
	require.NoError(t, err)
	f.run(t, receipt.JobID)
	require.Empty(t, f.status(t, receipt.JobID).Jobs[0].Job.ChangeID)
}
