package publish

import (
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestBodyUsesSelectedEvidenceAndGeneratedIdentity(t *testing.T) {
	source := record.Source{Commit: record.ObjectID(strings.Repeat("a", 40))}
	change := record.Change{GeneratedCommit: source.Commit}
	content := record.PublicationContent{Title: "fixture: update to 2", Body: "Useful explanation\n\nGenerated-by: [dockhand](https://github.com/herbygillot/dockhand)"}
	attempt := record.Attempt{ID: "earlier-reused-attempt", Spec: record.BuildSpec{Target: record.Target{Name: "fixture-subport", Portfile: "devel/fixture/Portfile"}, Config: record.BuildConfig{FromSource: false, Tests: record.TestDeclared}}, Evidence: &record.Evidence{Verdict: record.VerdictPassed, ObservedAt: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC), Environment: &record.EnvironmentEvidence{Provider: "tart", ProviderVersion: "2.30", Image: "recorded-image", EnvironmentDigest: "sha256:recorded", Guest: &record.GuestEnvironment{MacOSVersion: "26.1", MacOSBuild: "25B77", Architecture: "arm64", DeveloperTools: record.DeveloperToolsXcode, DeveloperToolsVersion: "Xcode 26.1\nBuild version 17B12", NoActivePorts: true, NoForeignPackageManagers: true}}}}
	for _, phase := range []string{"lint", "test", "install"} {
		args := []string{"/opt/local/bin/port", "-N", "-D", "/var/tmp/dockhand2/ports/devel/fixture"}
		if phase != "lint" {
			args = append(args, "-d")
		}
		args = append(args, phase, "subport=fixture-subport", "+debug", "-universal")
		attempt.Evidence.Steps = append(attempt.Evidence.Steps, record.StepResult{Package: "fixture-subport", Phase: phase, Verdict: record.VerdictPassed, Command: args, User: "root"})
	}
	body := publicationBody(content, change, source, attempt)
	for _, want := range []string{"Submitted by [dockhand]", "Useful explanation", "macOS 26.1; build 25B77; arm64", "Xcode 26.1 Build version 17B12", "version: 2.30; image: recorded-image", "earlier-reused-attempt", "2025-01-02 03:04:05 UTC", "[x] Squashed", "[x] Checked the Portfile", "[x] Ran the port's tests", "[x] Completed a full install", "-N -D devel/fixture -d install subport=fixture-subport +debug -universal", "run as root", "[ ] Followed", "[ ] Checked for other open", "[ ] Referenced", "[ ] Tested basic functionality", "[ ] Checked the port's most important"} {
		require.Contains(t, body, want)
	}
	require.NotContains(t, body, "Generated-by:")
	require.NotContains(t, body, " -s ")
	require.NotContains(t, body, "pristine")
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
	require.Contains(t, body, "macOS not recorded")
	require.Contains(t, body, "Command Line Tools: 26.0.0.0.1")
	require.NotContains(t, body, "no active MacPorts ports")
}
