package publish

import (
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestBodyUsesSelectedEvidenceAndGeneratedIdentity(t *testing.T) {
	t.Parallel()
	source := record.Source{Commit: record.ObjectID(strings.Repeat("a", 40))}
	change := record.Change{GeneratedCommit: source.Commit}
	content := record.PublicationContent{Title: "fixture: update to 2", Body: "Useful explanation\n\nGenerated-By: Dockhand devel+1a2b3c4d5e6f (https://github.com/herbygillot/dockhand)\nAssisted-By: Dockhand devel+1a2b3c4d5e6f (https://github.com/herbygillot/dockhand)\nGenerated-by: [dockhand](https://github.com/herbygillot/dockhand)"}
	attempt := record.Attempt{ID: "earlier-reused-attempt", Spec: record.BuildSpec{Target: record.Target{Name: "fixture-subport", Portfile: "devel/fixture/Portfile"}, Config: record.BuildConfig{FromSource: false, Tests: record.TestDeclared}}, Evidence: &record.Evidence{Verdict: record.VerdictPassed, ObservedAt: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC), Environment: &record.EnvironmentEvidence{Provider: "tart", ProviderVersion: "2.30", Image: "recorded-image", EnvironmentDigest: "sha256:recorded", Guest: &record.GuestEnvironment{MacOSVersion: "26.1", MacOSBuild: "25B77", Architecture: "arm64", DeveloperTools: record.DeveloperToolsXcode, DeveloperToolsVersion: "Xcode 26.1\nBuild version 17B12", CommandLineToolsVersion: "26.1.0.0.1", NoActivePorts: true, NoForeignPackageManagers: true}}}}
	for _, phase := range []string{"lint", "test", "install"} {
		args := []string{"/opt/local/bin/port", "-N", "-D", "/var/tmp/dockhand2/ports/devel/fixture"}
		if phase != "lint" {
			args = append(args, "-d")
		}
		args = append(args, phase, "subport=fixture-subport", "+debug", "-universal")
		attempt.Evidence.Steps = append(attempt.Evidence.Steps, record.StepResult{Package: "fixture-subport", Phase: phase, Verdict: record.VerdictPassed, Command: args, User: "root"})
	}
	body := publicationBody(content, change, source, attempt)
	for _, want := range []string{"Submitted by **[dockhand]", "Useful explanation", "| macOS 26.1 | build 25B77; arm64 |", "| Xcode | 26.1 Build version 17B12 |", "Provider: tart\n\n- version: 2.30\n- image: recorded-image (pristine)\n", "| Command Line Tools | 26.1.0.0.1 |", "Unchecked manual items require contributor review.", "earlier-reused-attempt", "2025-01-02 03:04:05 UTC", "[x] Squashed", "[x] Checked the Portfile", "[x] Ran the port's tests", "[x] Completed a full install", "-N -D devel/fixture -d install subport=fixture-subport +debug -universal", "run as root", "[ ] Followed", "[ ] Checked for other open", "[ ] Referenced", "[ ] Tested basic functionality", "[ ] Checked the port's most important"} {
		require.Contains(t, body, want)
	}
	require.NotContains(t, body, "Generated-by:")
	require.NotContains(t, body, "Generated-By:")
	require.NotContains(t, body, "Assisted-By:")
	require.NotContains(t, body, " -s ")
	require.NotContains(t, body, "Before verification:")
	require.NotContains(t, body, "Command paths above")
	// Only recorded argv can establish source-only installation, not current flags.
	attempt.Spec.Config.FromSource = true
	require.NotContains(t, publicationBody(content, change, source, attempt), " -s ")
	attempt.Evidence.Steps[2].Command = append([]string{"/opt/local/bin/port", "-s"}, attempt.Evidence.Steps[2].Command[1:]...)
	require.Contains(t, publicationBody(content, change, source, attempt), "port -s -N")
	for _, generated := range []record.ObjectID{"", record.ObjectID(strings.Repeat("b", 40))} {
		change.GeneratedCommit = generated
		require.Contains(t, publicationBody(content, change, source, attempt), "[ ] Squashed")
	}
}

func TestBodyDoesNotInventTestsOrEnvironmentForOlderEvidence(t *testing.T) {
	t.Parallel()
	attempt := record.Attempt{ID: "original", Spec: record.BuildSpec{Target: record.Target{Name: "fixture"}, Config: record.BuildConfig{Platform: record.Platform{OS: "darwin", Version: "25"}, Tests: record.TestDeclared}}, Evidence: &record.Evidence{Verdict: record.VerdictPassed, ObservedAt: time.Now(), Steps: []record.StepResult{{Package: "other", Phase: "test", Verdict: record.VerdictPassed}, {Package: "fixture", Phase: "install", Verdict: record.VerdictPassed}}}}
	for _, reason := range []string{"", "Skipped by request", "Port declares no test phase"} {
		attempt.Evidence.TestOmission = reason
		body := publicationBody(record.PublicationContent{Title: "fixture: update"}, record.Change{}, record.Source{}, attempt)
		require.Contains(t, body, "Environment details were not recorded")
		require.Contains(t, body, "[ ] Ran the port's tests")
		require.Contains(t, body, "[x] Completed a full install (exact command was not recorded)")
		require.NotContains(t, body, "macOS 26")
		require.Contains(t, body, reason)
	}
	attempt.Evidence.Environment = &record.EnvironmentEvidence{Guest: &record.GuestEnvironment{DeveloperTools: record.DeveloperToolsCommandLine, DeveloperToolsVersion: "26.0.0.0.1"}}
	body := publicationBody(record.PublicationContent{}, record.Change{}, record.Source{}, attempt)
	require.NotContains(t, body, "macOS", "a guest with no recorded macOS gets no row, rather than a row of blanks")
	require.Contains(t, body, "| Command Line Tools | 26.0.0.0.1 |")
	require.NotContains(t, body, "no active MacPorts ports")
	for _, guest := range []*record.GuestEnvironment{nil, {}, {NoActivePorts: true}, {NoForeignPackageManagers: true}} {
		attempt.Evidence.Environment.Image = "recorded-image"
		attempt.Evidence.Environment.Guest = guest
		require.NotContains(t, publicationBody(record.PublicationContent{}, record.Change{}, record.Source{}, attempt), "(pristine)")
	}
}

func TestBodyReportsWorkflowSuccessWithoutInventingPortPhases(t *testing.T) {
	t.Parallel()
	evidence := &record.Evidence{Verdict: record.VerdictPassed, ObservedAt: time.Now(), TestOmission: "Workflow policy may tolerate test failures", Workflow: &record.WorkflowEvidence{RunID: 10, RunAttempt: 2, URL: "https://github.com/author/ports/actions/runs/10", Jobs: []record.WorkflowJob{{Name: "macos-15", Conclusion: "success"}}}}
	body := publicationBody(record.PublicationContent{Title: "fixture: update"}, record.Change{}, record.Source{}, record.Attempt{ID: "attempt", Evidence: evidence})
	require.Contains(t, body, "Provider: GitHub Actions")
	require.Contains(t, body, "attempt 2")
	require.Contains(t, body, "macos-15: success")
	require.Contains(t, body, "may tolerate port test failures")
	require.NotContains(t, body, "[x] Ran the port's tests")
	require.NotContains(t, body, "[x] Completed a full install")
	require.NotContains(t, body, "image:")
}

func TestBodyDisclosesAnUnverifiedPublication(t *testing.T) {
	t.Parallel()
	source := record.Source{Commit: record.ObjectID(strings.Repeat("a", 40))}
	change := record.Change{GeneratedCommit: source.Commit}
	content := record.PublicationContent{Title: "fixture: update to 2", Body: "Useful explanation"}
	body := publicationBody(content, change, source, record.Attempt{})
	for _, want := range []string{"###### Tested on", "Not built locally", "`--skip-verify`", "no lint, test, or install verdict", "[ ] Checked the Portfile with lint (skipped at the author's request).", "[ ] Ran the port's tests (skipped at the author's request).", "[ ] Completed a full install (skipped at the author's request).", "[x] Squashed"} {
		require.Contains(t, body, want)
	}
	for _, absent := range []string{"Verification attempt:", "Environment details were not recorded", "no successful execution recorded"} {
		require.NotContains(t, body, absent)
	}
}

func TestBodyReportsAnAdvisoryTestFailure(t *testing.T) {
	t.Parallel()
	source := record.Source{Commit: record.ObjectID(strings.Repeat("a", 40))}
	attempt := record.Attempt{ID: "attempt", Spec: record.BuildSpec{Target: record.Target{Name: "fixture", Portfile: "devel/fixture/Portfile"}, Config: record.BuildConfig{Tests: record.TestDeclared}}, Evidence: &record.Evidence{Verdict: record.VerdictPassed, ObservedAt: time.Date(2026, 9, 19, 1, 2, 3, 0, time.UTC), TestFailure: "child process exited abnormally", Steps: []record.StepResult{{Package: "fixture", Phase: "lint", Verdict: record.VerdictPassed}, {Package: "fixture", Phase: "test", Verdict: record.VerdictFailed, Detail: "child process exited abnormally"}, {Package: "fixture", Phase: "install", Verdict: record.VerdictPassed}}}}
	body := publicationBody(record.PublicationContent{Title: "fixture: update to 2"}, record.Change{}, source, attempt)
	require.Contains(t, body, "[ ] Ran the port's tests — the port's tests failed; advisory here as in the MacPorts workflow: child process exited abnormally.")
	require.Contains(t, body, "[x] Completed a full install")
	require.NotContains(t, body, "no successful execution recorded")
}

// The environment is a table of facts with values, and the provider's own
// facts hang off the provider that reported them.
func TestTestedOnTablesTheEnvironmentAndBulletsTheProvider(t *testing.T) {
	t.Parallel()
	attempt := record.Attempt{ID: "attempt_1", Spec: record.BuildSpec{Target: record.Target{Name: "jc"}, Config: record.BuildConfig{Tests: record.TestDeclared}},
		Evidence: &record.Evidence{Verdict: record.VerdictPassed, ObservedAt: time.Date(2026, 9, 20, 10, 25, 56, 0, time.UTC), Dockhand: "v0.0.0-20260920.1",
			Environment: &record.EnvironmentEvidence{Provider: "tart", ProviderVersion: "2.37.0", Image: "dockhand-base-tahoe", EnvironmentDigest: "sha256:4602",
				Guest: &record.GuestEnvironment{MacOSVersion: "26.6.2", MacOSBuild: "25G83", Architecture: "arm64", DeveloperTools: record.DeveloperToolsCommandLine,
					DeveloperToolsVersion: "27.0.0.0.1788430756", MacPortsVersion: "Version: 2.12.6", NoActivePorts: true, NoForeignPackageManagers: true}}}}
	body := publicationBody(record.PublicationContent{Title: "jc: update to 1.26.0"}, record.Change{}, record.Source{}, attempt)
	require.Contains(t, body, "\n|  |  |\n| --- | --- |\n| macOS 26.6.2 | build 25G83; arm64 |\n| Command Line Tools | 27.0.0.0.1788430756 |\n| MacPorts | 2.12.6 |\n| dockhand | v0.0.0-20260920.1 |\n")
	require.Contains(t, body, "\nProvider: tart\n\n- version: 2.37.0\n- image: dockhand-base-tahoe (pristine)\n")
	require.Contains(t, body, "\nEnvironment identity: `sha256:4602`\n")

	// A workflow observation establishes no runner versions, so it tables only
	// the build that drove it and hangs the run and its jobs off the provider.
	attempt.Evidence.Environment = nil
	attempt.Evidence.Workflow = &record.WorkflowEvidence{URL: "https://github.com/author/ports/actions/runs/10", RunAttempt: 2,
		Jobs: []record.WorkflowJob{{Name: "macos-14", Conclusion: "success"}, {Name: "macos-15", Conclusion: "success"}}}
	body = publicationBody(record.PublicationContent{}, record.Change{}, record.Source{}, attempt)
	require.Contains(t, body, "\n|  |  |\n| --- | --- |\n| dockhand | v0.0.0-20260920.1 |\n")
	require.Contains(t, body, "\nProvider: GitHub Actions\n\n- [workflow run](https://github.com/author/ports/actions/runs/10), attempt 2\n- macos-14: success\n- macos-15: success\n")

	// Evidence from a build that recorded none of this writes no table.
	attempt.Evidence.Dockhand, attempt.Evidence.Workflow = "", nil
	require.NotContains(t, publicationBody(record.PublicationContent{}, record.Change{}, record.Source{}, attempt), "| --- | --- |")
}
