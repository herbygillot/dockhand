package tart

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

type driverFixture struct {
	DB, Repo   string
	Repository record.RepositoryID
	Job        record.JobID
	Config     Config
}

func TestTartDriverProcess(t *testing.T) {
	t.Parallel()
	path := os.Getenv("DOCKHAND_TART_DRIVER_FIXTURE")
	if path == "" {
		t.Skip("subprocess helper")
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var f driverFixture
	require.NoError(t, json.Unmarshal(data, &f))
	store, err := sqlite.Open(t.Context(), f.DB, sqlite.Options{})
	require.NoError(t, err)
	defer store.Close()
	repo, err := git.Open(t.Context(), f.Repo, "")
	require.NoError(t, err)
	p := &Provider{Config: f.Config, State: store, Repository: f.Repository, Repo: repo}
	engine := &workflow.Engine{State: state.Bind(store, record.Repository{ID: f.Repository}), Provider: p, WaitInterval: 100 * time.Millisecond, RetryDelay: 100 * time.Millisecond}
	scope := workflow.Scope{Jobs: []record.JobID{f.Job}}
	deadline := time.Now().Add(8 * time.Minute)
	for time.Now().Before(deadline) {
		cycle, err := engine.Cycle(t.Context(), scope)
		require.NoError(t, err)
		for _, problem := range cycle.Problems {
			t.Logf("cycle: %+v", problem)
		}
		status, err := engine.Status(t.Context(), scope)
		require.NoError(t, err)
		job := status.Jobs[0]
		if os.Getenv("DOCKHAND_TART_DRIVER_MODE") == "admit" && job.Job.AdmittedAt != nil {
			t.Logf("admitted %s", job.Attempts[0].Run.RunID)
			return
		}
		switch job.Job.State {
		case record.JobCompleted:
			require.Equal(t, "finish", os.Getenv("DOCKHAND_TART_DRIVER_MODE"), "admitting process should leave while the build is running")
			for _, resource := range status.Resources {
				require.Equal(t, record.ResourceReleased, resource.State)
			}
			t.Logf("completed %s", job.Attempts[0].Run.RunID)
			return
		case record.JobFailed, record.JobNeedsAttention, record.JobCanceled:
			t.Fatalf("job ended as %s: %s (%+v)", job.Job.State, job.Job.Detail, job.Attempts)
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("driver did not reach requested milestone")
}

func TestRealTartBuildSurvivesSubmittingDriverExit(t *testing.T) {
	t.Parallel()
	image := os.Getenv("DOCKHAND_TEST_TART_IMAGE")
	if image == "" {
		t.Skip("set DOCKHAND_TEST_TART_IMAGE to a prepared local VM for the opt-in integration test")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 14*time.Minute)
	defer cancel()
	directory, err := os.MkdirTemp("", "dockhand-tart-live-")
	require.NoError(t, err)
	t.Logf("live test data: %s", directory)
	sourceDir := filepath.Join(directory, "source")
	require.NoError(t, os.MkdirAll(filepath.Join(sourceDir, "devel/dockhand-fixture"), 0700))
	portfile := `PortSystem 1.0
name dockhand-fixture
version 1.0
revision 0
categories devel
license MIT
maintainers nomaintainer
description {Dockhand lifecycle fixture}
long_description ${description}
homepage https://example.invalid
platforms any
supported_archs noarch
distfiles
fetch {}
checksum {}
extract {}
use_configure no
build {
    file mkdir ${worksrcpath}
    set f [open ${worksrcpath}/dockhand-fixture w]
    puts $f {#!/bin/sh}
    puts $f {echo dockhand-fixture}
    close $f
}
test.run yes
test {
    after 8000
    if {![file exists ${worksrcpath}/dockhand-fixture]} { error {fixture was not built} }
}
destroot {
    xinstall -m 755 ${worksrcpath}/dockhand-fixture ${destroot}${prefix}/bin/dockhand-fixture
}
`
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "devel/dockhand-fixture/Portfile"), []byte(portfile), 0644))
	for _, args := range [][]string{{"init", "--quiet", "-b", "candidate"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "fixture"}} {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = sourceDir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	repo, err := git.Open(ctx, sourceDir, "")
	require.NoError(t, err)
	db := filepath.Join(directory, "state.db")
	store, err := sqlite.Open(ctx, db, sqlite.Options{})
	require.NoError(t, err)
	defer store.Close()
	repository, err := store.RegisterRepository(ctx, repo.CommonDir)
	require.NoError(t, err)
	ports := &eval.Evaluator{Executable: testsupport.MacPortsTclsh(t)}
	platform, err := ports.NativePlatform(ctx)
	require.NoError(t, err)
	config := Config{Image: image, ArtifactDirectory: filepath.Join(directory, "artifacts"), Platform: platform, Capacity: 1, PortIndexExecutable: testsupport.MacPortsTool(t, "portindex")}
	provider := &Provider{Config: config, State: store, Repository: repository.ID, Repo: repo}
	t.Log("hashing prepared VM image")
	environment, err := provider.describeEnvironment(ctx)
	require.NoError(t, err)
	t.Logf("environment %s", environment.Digest)
	engine := &workflow.Engine{State: state.Bind(store, repository), Repo: repo, Ports: ports, Provider: provider}
	bound, err := engine.BindVerification(ctx, workflow.VerificationRequest{ID: "live", Branch: "candidate", Selection: macports.Selection{Selector: "dockhand-fixture"}, Build: record.BuildConfig{Provider: "tart", Platform: platform, EnvironmentDigest: environment.Digest, CapabilitiesRequired: true, FromSource: true, Tests: record.TestDeclared}})
	require.NoError(t, err)
	receipt, err := engine.Submit(ctx, bound.Request)
	require.NoError(t, err)
	scope := workflow.Scope{Jobs: []record.JobID{receipt.JobID}}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		status, err := engine.Status(cleanup, scope)
		if err != nil {
			t.Logf("cleanup status: %v", err)
			return
		}
		for _, attempt := range status.Jobs[0].Attempts {
			if attempt.Run.RunID != "" {
				if err := provider.Cancel(cleanup, attempt.Run); err != nil {
					t.Logf("cleanup cancel: %v", err)
				}
			} else {
				if _, err := provider.Reconcile(cleanup, attempt.SubmissionID, verify.ReconcileOptions{}); err != nil {
					t.Logf("cleanup reconcile: %v", err)
				}
			}
			if _, err := provider.Release(cleanup, record.ResourceHandle{Provider: "tart", ID: string(attempt.SubmissionID)}); err != nil {
				t.Logf("cleanup release: %v", err)
			}
		}
	}()
	f := driverFixture{DB: db, Repo: sourceDir, Repository: repository.ID, Job: receipt.JobID, Config: config}
	raw, err := json.Marshal(f)
	require.NoError(t, err)
	fixture := filepath.Join(directory, "driver.json")
	require.NoError(t, os.WriteFile(fixture, raw, 0600))
	run := func(mode string) {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTartDriverProcess$", "-test.v", "-test.timeout=10m")
		cmd.Env = append(os.Environ(), "DOCKHAND_TART_DRIVER_FIXTURE="+fixture, "DOCKHAND_TART_DRIVER_MODE="+mode)
		output, err := cmd.CombinedOutput()
		t.Logf("%s process:\n%s", mode, output)
		require.NoError(t, err)
	}
	run("admit")
	status, err := engine.Status(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, record.JobActive, status.Jobs[0].Job.State)
	firstRun := status.Jobs[0].Attempts[0].Run
	run("finish")
	status, err = engine.Status(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, record.JobCompleted, status.Jobs[0].Job.State)
	require.Equal(t, firstRun, status.Jobs[0].Attempts[0].Run)
	require.Equal(t, record.VerdictPassed, status.Jobs[0].Attempts[0].Evidence.Verdict)
	require.NotNil(t, status.Jobs[0].Attempts[0].Evidence.Environment)
	require.Equal(t, environment.Digest, status.Jobs[0].Attempts[0].Evidence.Environment.EnvironmentDigest)
	require.NotEmpty(t, status.Jobs[0].Attempts[0].Evidence.Logs)
	evidence := status.Jobs[0].Attempts[0].Evidence
	require.NotNil(t, evidence.Environment.Guest)
	require.NotEmpty(t, evidence.Environment.Guest.MacOSVersion)
	require.NotEmpty(t, evidence.Environment.Guest.MacOSBuild)
	require.NotEmpty(t, evidence.Environment.Guest.DeveloperToolsVersion)
	require.NotEmpty(t, evidence.Environment.ProviderVersion)
	require.Equal(t, image, evidence.Environment.Image)
	require.True(t, evidence.Environment.Guest.NoActivePorts)
	require.True(t, evidence.Environment.Guest.NoForeignPackageManagers)
	require.Empty(t, evidence.TestOmission)
	require.Len(t, evidence.Steps, 4)
	for _, step := range evidence.Steps {
		require.Equal(t, "root", step.User)
		require.Contains(t, step.Command, "-s")
		if step.Phase == "lint" {
			require.NotContains(t, step.Command, "-d")
		} else {
			require.Contains(t, step.Command, "-d")
		}
	}
	metadata, _ := json.Marshal(evidence.Environment)
	t.Logf("observed environment: %s", metadata)
	fmt.Fprintf(os.Stdout, "LIVE_TART_RESULT=%s run=%s\n", directory, firstRun.RunID)
}
