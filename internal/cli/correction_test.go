package cli

import (
	"bytes"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAmendCLIReusesVerificationAndKeepsContribution(t *testing.T) {
	t.Parallel()
	config, repo, _ := preparationCLI(t)
	configureReuseImage(t, &config)
	var out, stderr bytes.Buffer
	require.NoError(t, runFixture(t.Context(), []string{"bump-revision", "fixture", "--subject", "rebuild", "--to", "branch", "--json"}, Streams{Out: &out, Err: &stderr}, config))
	var original ActionResult
	decodeResult(t, out.Bytes(), &original)
	branch := original.Status.Jobs[0].Job.Prepared.Branch
	seedCLIVerification(t, config, branch)
	before, err := repo.ReadRefs(t.Context(), "refs/heads/")
	require.NoError(t, err)
	var preview bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"amend", "--branch", branch, "--dry-run"}, Streams{Out: &preview, Err: &stderr}, config))
	require.Empty(t, preview.String())
	after, err := repo.ReadRefs(t.Context(), "refs/heads/")
	require.NoError(t, err)
	require.Equal(t, before, after)
	out.Reset()
	stderr.Reset()
	require.NoError(t, runFixture(t.Context(), []string{"amend", "--branch", branch, "--provider", "tart", "--to", "verified", "--json"}, Streams{Out: &out, Err: &stderr}, config), stderr.String())
	var result ActionResult
	decodeResult(t, out.Bytes(), &result)
	require.Equal(t, record.JobCompleted, result.Status.Jobs[0].Job.State)
	require.Equal(t, original.Status.Jobs[0].Job.ChangeID, result.Status.Jobs[0].Job.ChangeID)
	require.NotEmpty(t, result.Status.Jobs[0].Job.ReusedAttempt)
}
func TestCorrectionPreviewDoesNotInitializeMissingState(t *testing.T) {
	t.Parallel()
	config, _, _ := preparationCLI(t)
	var out bytes.Buffer
	err := Run(t.Context(), []string{"amend", "--dry-run"}, Streams{Out: &out, Err: &out}, config)
	require.Error(t, err)
	require.NoFileExists(t, config.DBPath)
}
