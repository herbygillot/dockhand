package view

import (
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/record"
)

// Evidence is what one verification established, in words. The status
// listing, the completion summary, and the pull request's Tested on section
// each lay these facts out their own way and read nothing else from the
// record, so a change in what a build reports is worded here once.
type Evidence struct {
	// Verdict is the recorded verdict, empty while the build runs.
	Verdict string
	// Platform is the build platform in words, "macOS 26 arm64", when the
	// attempt is known; a reused observation names none.
	Platform   string
	ObservedAt time.Time
	// Provider is where the build ran: GitHub Actions for a workflow
	// observation, otherwise the provider the environment names.
	Provider string
	// Workflow is the run a workflow observation came from.
	Workflow *Workflow
	// Environment is the admitted environment a local provider observed.
	Environment *Environment
	// Components are the versions the build ran on, in the order a table
	// lists them: macOS, the developer tools, MacPorts, and the dockhand
	// that drove it; only what was recorded appears.
	Components []Component
	// Failure is what failed and where, when the build failed.
	Failure *Failure
	// Log is the location of the build's first log, when one was kept.
	Log string
	// TestOmission says why the declared tests did not run; TestFailure
	// how they failed when the policy let the build pass regardless.
	TestOmission, TestFailure string
	// Steps are the phases the build ran, in order, as recorded.
	Steps []Step
}

// Step is one phase of a build as recorded: what it ran, for which
// package, as whom, and how it ended.
type Step struct {
	Phase   string
	Package string
	Verdict string
	Command []string
	User    string
}

// Passed is the recorded step that passed the phase for the package, the
// last when the phase ran more than once, or nil.
func (e Evidence) Passed(phase, pkg string) *Step {
	var passed *Step
	for i := range e.Steps {
		if step := &e.Steps[i]; step.Phase == phase && step.Package == pkg && step.Verdict == string(record.VerdictPassed) {
			passed = step
		}
	}
	return passed
}

// Workflow is a GitHub Actions run as evidence: its outcome, its identity,
// the fork branch it built, and its jobs.
type Workflow struct {
	// Outcome is the run's conclusion, or its status while it has none.
	Outcome    string
	RunID      int64
	RunAttempt int
	URL        string
	Repository string
	Branch     string
	Commit     string
	Jobs       []WorkflowJob
}

// WorkflowJob is one job of the run with its status, conclusion, and page.
type WorkflowJob struct {
	Name, Status, Conclusion, URL string
}

// Environment is the admitted environment a local provider built in.
type Environment struct {
	Provider string
	// Version is the provider's own version; Image the image it booted,
	// Pristine when the guest reported no active ports and no foreign
	// package manager.
	Version  string
	Image    string
	Pristine bool
	// Identity and CapabilityIdentity are the environment's digests.
	Identity, CapabilityIdentity string
	// MacPortsVersion and MacPortsPrefix are the admitted MacPorts;
	// DeveloperTools names the tools with their Xcode version when known.
	MacPortsVersion, MacPortsPrefix, DeveloperTools string
	// GuestRecorded is whether the run recorded the guest's own versions.
	GuestRecorded bool
}

// Component is one row of what the build ran on.
type Component struct {
	Name, Version string
}

// Failure is what failed and where.
type Failure struct {
	Kind, Package, Phase, Detail string
	// Fetches are the mirrors tried for a distfile that failed to fetch,
	// each as "url: reason".
	Fetches []string
}

// Platform words a build platform.
func Platform(platform record.Platform) string {
	return macos.Describe(platform)
}

// Verification words an attempt's evidence with the platform it ran on.
func Verification(attempt record.Attempt) Evidence {
	facts := Facts(attempt.Evidence)
	facts.Platform = Platform(attempt.Spec.Config.Platform)
	return facts
}

// Facts words recorded evidence; nil evidence is an empty Evidence.
func Facts(evidence *record.Evidence) Evidence {
	var facts Evidence
	if evidence == nil {
		return facts
	}
	facts.Verdict = string(evidence.Verdict)
	facts.ObservedAt = evidence.ObservedAt
	facts.TestOmission = evidence.TestOmission
	facts.TestFailure = evidence.TestFailure
	for _, step := range evidence.Steps {
		facts.Steps = append(facts.Steps, Step{Phase: step.Phase, Package: step.Package, Verdict: string(step.Verdict), Command: step.Command, User: step.User})
	}
	if flow := evidence.Workflow; flow != nil {
		facts.Provider = "GitHub Actions"
		run := &Workflow{Outcome: flow.Conclusion, RunID: flow.RunID, RunAttempt: flow.RunAttempt, URL: flow.URL, Repository: flow.Repository, Branch: flow.Branch, Commit: string(flow.Commit)}
		if run.Outcome == "" {
			run.Outcome = flow.Status
		}
		for _, job := range flow.Jobs {
			run.Jobs = append(run.Jobs, WorkflowJob{Name: job.Name, Status: job.Status, Conclusion: job.Conclusion, URL: job.URL})
		}
		facts.Workflow = run
	}
	if observed := evidence.Environment; observed != nil {
		tools := string(observed.Capabilities.DeveloperTools)
		if observed.Capabilities.XcodeVersion != "" {
			tools += " " + observed.Capabilities.XcodeVersion
		}
		environment := &Environment{Provider: observed.Provider, Version: observed.ProviderVersion, Image: observed.Image, Identity: observed.EnvironmentDigest, CapabilityIdentity: observed.CapabilityDigest, MacPortsVersion: observed.Capabilities.MacPortsVersion, MacPortsPrefix: observed.Capabilities.MacPortsPrefix, DeveloperTools: tools}
		if guest := observed.Guest; guest != nil {
			environment.GuestRecorded = true
			environment.Pristine = strings.TrimSpace(observed.Image) != "" && guest.NoActivePorts && guest.NoForeignPackageManagers
			facts.Components = guestComponents(guest)
		}
		if facts.Provider == "" {
			facts.Provider = observed.Provider
		}
		facts.Environment = environment
	}
	if strings.TrimSpace(evidence.Dockhand) != "" {
		facts.Components = append(facts.Components, Component{"dockhand", oneLine(evidence.Dockhand)})
	}
	if failure := evidence.Failure; failure != nil {
		words := &Failure{Kind: string(failure.Kind), Package: failure.Package, Phase: failure.Phase, Detail: failure.Detail}
		for _, fetch := range failure.Fetches {
			words.Fetches = append(words.Fetches, fetch.URL+": "+fetch.Reason)
		}
		facts.Failure = words
	}
	for _, log := range evidence.Logs {
		if log.Location != "" {
			facts.Log = log.Location
			break
		}
	}
	return facts
}

// guestComponents lists the guest's macOS, developer tools, and MacPorts,
// each only when recorded.
func guestComponents(guest *record.GuestEnvironment) []Component {
	var rows []Component
	add := func(name, version string) {
		if strings.TrimSpace(version) != "" {
			rows = append(rows, Component{name, oneLine(version)})
		}
	}
	if strings.TrimSpace(guest.MacOSVersion) != "" {
		add("macOS", oneLine(guest.MacOSVersion)+" (build "+notRecorded(guest.MacOSBuild)+"; "+notRecorded(guest.Architecture)+")")
	}
	switch guest.DeveloperTools {
	case record.DeveloperToolsXcode:
		add("Xcode", strings.TrimPrefix(oneLine(guest.DeveloperToolsVersion), "Xcode "))
		add("Command Line Tools", guest.CommandLineToolsVersion)
	case record.DeveloperToolsCommandLine:
		add("Command Line Tools", guest.DeveloperToolsVersion)
	default:
		add("Developer tools", guest.DeveloperToolsVersion)
		add("Command Line Tools", guest.CommandLineToolsVersion)
	}
	add("MacPorts", strings.TrimPrefix(guest.MacPortsVersion, "Version: "))
	return rows
}

// notRecorded is the value of a fact the build did not record.
func notRecorded(s string) string {
	if strings.TrimSpace(s) == "" {
		return "not recorded"
	}
	return oneLine(s)
}

// FailureWords is the failure as a clause after the verdict: the phase, the
// package when it is not the target, and what MacPorts said.
func (e Evidence) FailureWords(target string) string {
	failure := e.Failure
	if failure == nil {
		return ""
	}
	var text string
	if failure.Phase != "" {
		text += "; " + failure.Phase + " phase"
	}
	if failure.Package != "" && failure.Package != target {
		text += " of " + failure.Package
	}
	if failure.Detail != "" {
		text += ": " + failure.Detail
	}
	return text
}

// AttemptWords is one build's verdict or progress on its platform, with the
// failing phase and the log location when it failed. A target other than
// the job's first is named first.
func AttemptWords(job record.Job, attempt record.Attempt) string {
	facts := Verification(attempt)
	var prefix string
	if name := attempt.Spec.Target.Name; name != "" && (len(job.Spec.Targets) == 0 || name != job.Spec.Targets[0].Name) {
		prefix = name + ": "
	}
	if facts.Verdict != "" {
		text := prefix + facts.Verdict + " on " + facts.Platform + facts.FailureWords(attempt.Spec.Target.Name)
		if facts.Log != "" {
			text += "; log: " + facts.Log
		}
		return text
	}
	var text string
	switch attempt.State {
	case record.AttemptQueued:
		text = "waiting for a build slot on " + facts.Platform
	case record.AttemptSubmitting, record.AttemptRunning:
		text = "building on " + facts.Platform
	case record.AttemptUncertain:
		text = "outcome uncertain on " + facts.Platform
	default:
		text = string(attempt.State) + " on " + facts.Platform
	}
	if attempt.LastError != "" {
		text += "; " + attempt.LastError
	}
	return prefix + text
}
