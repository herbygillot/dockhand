package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	tartvm "github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/verify"
	providertart "github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/stretchr/testify/require"
)

func TestVerifyCLIReusesEvidenceAndFreshFlagIsDurable(t *testing.T) {
	config, _, _ := preparationCLI(t)
	configureReuseImage(t, &config)
	observed := seedCLIVerification(t, config, "candidate")
	var stdout, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"verify", "fixture", "--branch", "candidate", "--json"}, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
	var result ActionResult
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	entry := result.Status.Jobs[0]
	require.Equal(t, record.JobCompleted, entry.Job.State)
	require.Equal(t, record.AttemptID("original-attempt"), entry.Job.ReusedAttempt)
	require.Empty(t, entry.Attempts)
	require.NotNil(t, entry.Reused)
	require.Equal(t, observed, entry.Reused.Evidence.ObservedAt)
	require.Contains(t, stderr.String(), "completed; verification passed (reused)")
	require.Contains(t, entry.Job.ReuseDetail, "Reused passing verification")
	stdout.Reset()
	stderr.Reset()
	require.NoError(t, Run(t.Context(), []string{"wait", string(entry.Job.ID), "--trace", "--json"}, Streams{Out: &stdout, Err: &stderr}, config))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	detach := &detachOnAcceptance{cancel: cancel}
	stdout.Reset()
	err := Run(ctx, []string{"verify", "fixture", "--branch", "candidate", "--fresh", "--json"}, Streams{Out: &stdout, Err: detach}, config)
	require.ErrorIs(t, err, context.Canceled)
	status, err := app.Status(t.Context(), config)
	require.NoError(t, err)
	require.Len(t, status.Jobs, 3)
	fresh := status.Jobs[len(status.Jobs)-1].Job
	require.True(t, fresh.Spec.FreshVerification)
	require.Empty(t, fresh.ReusedAttempt)
	require.Equal(t, record.JobQueued, fresh.State)
}

func configureReuseImage(t *testing.T, config *app.Config) {
	t.Helper()
	config.Tart.Home = t.TempDir()
	config.Tart.Image = "base"
	config.Tart.Executable = filepath.Join(t.TempDir(), "tart")
	require.NoError(t, os.WriteFile(config.Tart.Executable, []byte("#!/bin/sh\n[ \"$1\" = list ] || exit 1\nprintf '%s\\n' '[{\"Name\":\"base\",\"Source\":\"local\",\"State\":\"stopped\"}]'\n"), 0700))
	image := filepath.Join(config.Tart.Home, "vms", "base")
	require.NoError(t, os.MkdirAll(image, 0700))
	for _, name := range []string{"config.json", "disk.img", "nvram.bin"} {
		require.NoError(t, os.WriteFile(filepath.Join(image, name), []byte("fixture"), 0600))
	}
}

func seedCLIVerification(t *testing.T, config app.Config, branch string) time.Time {
	t.Helper()
	services, err := app.Build(t.Context(), config)
	require.NoError(t, err)
	bound, err := services.BindVerification(t.Context(), app.Verification{ID: "original", Branch: branch, Selection: macports.Selection{Selector: "fixture"}, Tests: record.TestDeclared})
	require.NoError(t, err)
	receipt, err := services.Workflow.Submit(t.Context(), bound.Request)
	require.NoError(t, err)
	observed := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, services.Workflow.State.Update(t.Context(), services.Workflow.Repository, func(ctx context.Context, tx state.Tx) error {
		job, err := tx.Job(ctx, receipt.JobID)
		if err != nil {
			return err
		}
		var revision record.Revision
		if job.Spec.InputRevision != "" {
			revision, err = tx.Revision(ctx, job.Spec.InputRevision)
			if err != nil {
				return err
			}
		}
		plan, build, err := verify.PlanSingle(job, revision)
		if err != nil {
			return err
		}
		if err := tx.PutPlan(ctx, plan); err != nil {
			return err
		}
		tools := record.DeveloperToolsCommandLine
		if build.Config.NeedsXcode {
			tools = record.DeveloperToolsXcode
		}
		environment := &record.EnvironmentEvidence{
			Provider: build.Config.Provider, EnvironmentDigest: build.Config.EnvironmentDigest, CapabilityDigest: "sha256:fixture-capabilities",
			Capabilities: record.EnvironmentCapabilities{Platform: build.Config.Platform, MacPortsPrefix: "/opt/local", MacPortsVersion: "2.12.6", DeveloperTools: tools},
		}
		if err := tx.PutAttempt(ctx, record.Attempt{ID: "original-attempt", JobID: job.ID, TargetID: plan.Targets[0].ID, Spec: build, State: record.AttemptFinished, CreatedAt: job.AcceptedAt, Evidence: &record.Evidence{Verdict: record.VerdictPassed, Environment: environment, ObservedAt: observed}}); err != nil {
			return err
		}
		job.State = record.JobCompleted
		job.FinishedAt = &observed
		return tx.PutJob(ctx, job)
	}))
	require.NoError(t, services.Close())
	return observed
}

func TestVerifyCLISelectsSetupImageWhenImageIsOmitted(t *testing.T) {
	config, _, _ := preparationCLI(t)
	configureReuseImage(t, &config)
	evaluator := eval.Evaluator{Executable: config.TclExecutable, Prefix: config.MacPortsPrefix}
	platform, err := evaluator.NativePlatform(t.Context())
	require.NoError(t, err)
	name, err := tartvm.DefaultImageName(platform)
	require.NoError(t, err)
	require.NoError(t, os.Rename(filepath.Join(config.Tart.Home, "vms", "base"), filepath.Join(config.Tart.Home, "vms", name)))
	script, err := os.ReadFile(config.Tart.Executable)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(config.Tart.Executable, []byte(strings.ReplaceAll(string(script), "base", name)), 0700))
	config.Tart.Image = ""
	seedCLIVerification(t, config, "candidate")
	var stdout, stderr bytes.Buffer
	require.NoError(t, Run(t.Context(), []string{"verify", "fixture", "--branch", "candidate", "--wait", "--json"}, Streams{Out: &stdout, Err: &stderr}, config), "%s", stderr.String())
	var result ActionResult
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	require.Equal(t, record.JobCompleted, result.Status.Jobs[0].Job.State)
	require.Equal(t, record.AttemptID("original-attempt"), result.Status.Jobs[0].Job.ReusedAttempt)
	var providerConfig providertart.Config
	require.NoError(t, json.Unmarshal(result.Status.Jobs[0].Job.Spec.Build.ProviderConfig, &providerConfig))
	require.Equal(t, name, providerConfig.Image)
}
