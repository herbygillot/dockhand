package view_test

import (
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/workflow/view"
	"github.com/stretchr/testify/require"
)

func TestFactsWordALocalBuild(t *testing.T) {
	t.Parallel()
	observed := time.Date(2026, 9, 22, 1, 2, 3, 0, time.UTC)
	evidence := &record.Evidence{Verdict: record.VerdictFailed, ObservedAt: observed, Dockhand: "v0.0.0-20260920.1",
		Environment: &record.EnvironmentEvidence{Provider: "tart", ProviderVersion: "2.37.0", Image: "dockhand-base-tahoe", EnvironmentDigest: "sha256:4602", CapabilityDigest: "sha256:cap",
			Capabilities: record.EnvironmentCapabilities{MacPortsPrefix: "/opt/local", MacPortsVersion: "2.12.6", DeveloperTools: record.DeveloperToolsXcode, XcodeVersion: "26.6"},
			Guest:        &record.GuestEnvironment{MacOSVersion: "26.6.2", MacOSBuild: "25G83", Architecture: "arm64", DeveloperTools: record.DeveloperToolsXcode, DeveloperToolsVersion: "Xcode 26.1 Build version 17B12", CommandLineToolsVersion: "27.0", MacPortsVersion: "Version: 2.12.6", NoActivePorts: true, NoForeignPackageManagers: true}},
		Failure: &record.Failure{Kind: record.FailureKind("build"), Package: "libfoo", Phase: "fetch", Detail: "no mirror answered", Fetches: []record.FetchAttempt{{URL: "https://a/x.tgz", Reason: "404"}}},
		Logs:    []record.Artifact{{Name: "build", Location: "/tmp/attempt/build.log"}}}
	facts := view.Facts(evidence)
	require.Equal(t, "failed", facts.Verdict)
	require.Equal(t, observed, facts.ObservedAt)
	require.Equal(t, "tart", facts.Provider)
	require.Nil(t, facts.Workflow)
	require.Equal(t, &view.Environment{Provider: "tart", Version: "2.37.0", Image: "dockhand-base-tahoe", Pristine: true, Identity: "sha256:4602", CapabilityIdentity: "sha256:cap", MacPortsVersion: "2.12.6", MacPortsPrefix: "/opt/local", DeveloperTools: "xcode 26.6", GuestRecorded: true}, facts.Environment)
	require.Equal(t, []view.Component{{"macOS", "26.6.2 (build 25G83; arm64)"}, {"Xcode", "26.1 Build version 17B12"}, {"Command Line Tools", "27.0"}, {"MacPorts", "2.12.6"}, {"dockhand", "v0.0.0-20260920.1"}}, facts.Components)
	require.Equal(t, &view.Failure{Kind: "build", Package: "libfoo", Phase: "fetch", Detail: "no mirror answered", Fetches: []string{"https://a/x.tgz: 404"}}, facts.Failure)
	require.Equal(t, "; fetch phase of libfoo: no mirror answered", facts.FailureWords("jq"))
	require.Equal(t, "; fetch phase: no mirror answered", facts.FailureWords("libfoo"))
	require.Equal(t, "/tmp/attempt/build.log", facts.Log)
	require.Empty(t, view.Facts(nil).Components, "nothing recorded, nothing worded")
}

func TestFactsWordAWorkflowRun(t *testing.T) {
	t.Parallel()
	evidence := &record.Evidence{Verdict: record.VerdictPassed, Dockhand: "v1",
		Workflow: &record.WorkflowEvidence{Repository: "owner/ports", Branch: "update", Commit: "abc", RunID: 10, RunAttempt: 2, URL: "https://run", Status: "queued",
			Jobs: []record.WorkflowJob{{Name: "macos-14", Status: "completed", Conclusion: "success", URL: "https://job"}}}}
	facts := view.Facts(evidence)
	require.Equal(t, "GitHub Actions", facts.Provider)
	require.Equal(t, "queued", facts.Workflow.Outcome, "a run without a conclusion is worded by its status")
	require.Equal(t, "owner/ports", facts.Workflow.Repository)
	require.Equal(t, []view.WorkflowJob{{Name: "macos-14", Status: "completed", Conclusion: "success", URL: "https://job"}}, facts.Workflow.Jobs)
	require.Equal(t, []view.Component{{"dockhand", "v1"}}, facts.Components, "a workflow observation has no guest to list")
	evidence.Workflow.Conclusion = "success"
	require.Equal(t, "success", view.Facts(evidence).Workflow.Outcome)
}

func TestAttemptWordsCoverVerdictsAndProgress(t *testing.T) {
	t.Parallel()
	job := record.Job{Spec: record.JobSpec{Targets: []record.Target{{Name: "jq"}}}}
	platform := record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	attempt := record.Attempt{State: record.AttemptQueued, Spec: record.BuildSpec{Target: record.Target{Name: "jq"}, Config: record.BuildConfig{Platform: platform}}}
	require.Equal(t, "waiting for a build slot on macOS 26 (Tahoe) arm64", view.AttemptWords(job, attempt))
	attempt.State, attempt.LastError = record.AttemptRunning, ""
	require.Equal(t, "building on macOS 26 (Tahoe) arm64", view.AttemptWords(job, attempt))
	attempt.Spec.Target.Name = "jq-devel"
	attempt.State, attempt.LastError = record.AttemptUncertain, "provider lost"
	require.Equal(t, "jq-devel: outcome uncertain on macOS 26 (Tahoe) arm64; provider lost", view.AttemptWords(job, attempt))
	attempt.Spec.Target.Name = "jq"
	attempt.Evidence = &record.Evidence{Verdict: record.VerdictFailed, Failure: &record.Failure{Phase: "build", Detail: "error: linking failed"}, Logs: []record.Artifact{{Location: "/tmp/attempt/build.log"}}}
	require.Equal(t, "failed on macOS 26 (Tahoe) arm64; build phase: error: linking failed; log: /tmp/attempt/build.log", view.AttemptWords(job, attempt))
}
