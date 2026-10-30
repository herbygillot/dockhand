package view

import (
	"fmt"
	"io"
	"strings"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

func PortChangeWords(port engine.PortDiff) string {
	return strings.ReplaceAll(string(port.Change()), "_", " ")
}

func OrNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

// ComparisonLines are what each port's assessment found, a line each,
// marked as update marks them, under an Upstream label. Where there are
// several ports, each line names the ports that found it, said once for
// all of them: a python port's subports each repeated a license finding
// and a requirement's, whose other lines differed by provider (the Vx
// port's field testing, 2026-10-04, py-coremltools and py-mlx-vlm).
func ComparisonLines(comparisons []engine.PortComparison) []string {
	var lines []string
	several := len(engine.Grouped(comparisons)) > 1
	for _, l := range engine.ComparisonLines(comparisons) {
		mark, port := Unmarked+" ", ""
		if l.Hold {
			mark = Look + " held: "
		}
		if several {
			port = strings.Join(l.Ports, ", ") + ": "
		}
		lines = append(lines, mark+port+engine.WithRule(l.Words, l.Rule))
	}
	return lines
}

func PushWords(plan engine.SubmitPlan) string {
	switch plan.Push() {
	case engine.PushOthersPushed:
		return "won't push: someone else pushed " + plan.OthersPushed.Short()
	case engine.PushNothingNew:
		return "nothing new; the fork already has " + model.ObjectID(plan.Commit).Short()
	case engine.PushReplaces:
		if plan.Amended != "" {
			return fmt.Sprintf("replaces %s, someone else's commit, which this branch's reflog shows and %s amends, only if the remote still points to %s", plan.Amended.Short(), model.ObjectID(plan.Commit).Short(), plan.Amended.Short())
		}
		return fmt.Sprintf("replaces the fork branch at %s with %s, only if the remote still points to %s", model.ObjectID(plan.RemoteHead.Object).Short(), model.ObjectID(plan.Commit).Short(), model.ObjectID(plan.RemoteHead.Object).Short())
	case engine.PushNoFork:
		return "nothing: no fork to push to"
	case engine.PushAdds:
		return fmt.Sprintf("adds to the fork's branch, up to %s", model.ObjectID(plan.Commit).Short())
	}
	return "creates the fork's branch at " + model.ObjectID(plan.Commit).Short()
}

func CheckWords(plan engine.SubmitPlan) string {
	evidence := plan.Evidence
	switch {
	case plan.Request.NoCheck:
		return "none (--no-check); the pull request says MacPorts CI is its only check"
	case plan.CheckNeeded:
		return "runs first; it is submitted only if it passes"
	case evidence == nil:
		return "none has finished for this commit's files"
	}
	var environments, tested, excluded []string
	for i, environment := range evidence.Plan.Environments {
		words := engine.DescribeEnvironment(environment)
		environments = append(environments, words)
		switch {
		case evidence.Tested(i):
			tested = append(tested, words)
		case evidence.ProfileSkips(i):
			// The profile's choice, as minmax's past a port's oldest and
			// newest releases, isn't the ports' exclusion, and the table
			// leaves it out too (field testing at 9c5919dd: tart's line
			// said macOS 26, which minmax skipped, was excluded).
		case evidence.ExcludesAll(i):
			excluded = append(excluded, words)
		}
	}
	failed := evidence.Failed()
	if len(failed) == 0 {
		passed := fmt.Sprintf("passed on %s for this commit's files (%s)", strings.Join(tested, ", "), evidence.Run.Name())
		if len(excluded) > 0 {
			passed += fmt.Sprintf("; nothing built on %s, where every port's Portfile excludes it", strings.Join(excluded, ", "))
		}
		// What MacPorts' CI builds on that this check didn't: tart 2.40.1
		// passed on 26 alone and failed CI on 15 (field testing, #35157).
		if len(plan.UncoveredCI) > 0 {
			passed += fmt.Sprintf("; MacPorts CI also builds on macOS %s, which no check here built on (--build macports-ci checks those)", prose.And(plan.UncoveredCI))
		}
		return passed
	}
	// Each port is said where it failed and where nothing of these files
	// built it, apart: check-323 had passed grafana on macOS 12, and the
	// line said it "did not pass on" 12 and 26 alike (field testing,
	// grafana 13.2.4).
	var said []string
	for _, target := range failed {
		var failedOn, unbuiltOn []string
		for _, cell := range target.Outcomes {
			switch {
			case cell.Unbuilt():
				unbuiltOn = append(unbuiltOn, engine.DescribeEnvironment(cell.Environment))
			case cell.Kind == engine.CellRecorded && cell.Outcome != model.OutcomePassed:
				failedOn = append(failedOn, engine.DescribeEnvironment(cell.Environment))
			}
		}
		name := target.Target.Target.Name
		// An accepted failure says so, and why: the line listed
		// py-textual-fastdatatable's five accepted subports as "did not
		// pass" with no word of it (field testing, #35662).
		accepted := ""
		if why, ok := plan.Accepted[name]; ok {
			accepted = ", accepted: " + why.Words()
		}
		if len(failedOn) > 0 {
			said = append(said, fmt.Sprintf("%s did not pass on %s%s", name, strings.Join(failedOn, ", "), accepted))
		}
		if len(unbuiltOn) > 0 {
			said = append(said, fmt.Sprintf("%s isn't built for these files on %s", name, strings.Join(unbuiltOn, ", ")))
		}
		if len(failedOn) == 0 && len(unbuiltOn) == 0 {
			said = append(said, fmt.Sprintf("%s did not pass on %s", name, strings.Join(environments, ", ")))
		}
	}
	return fmt.Sprintf("%s: %s", evidence.Run.Name(), strings.Join(said, "; "))
}

// writeOthers names the port's other open pull requests the update found,
// or why it couldn't look. They stop nothing: submit shows them again, and
// bump and serve hold on one.
// WriteOthers names a port's other open pull requests, which stop nothing:
// submit shows them again, and bump and serve hold on one.
func WriteOthers(out io.Writer, port string, others []engine.OtherPullRequest) {
	for _, pr := range others {
		fmt.Fprintf(out, "Also open for %s: #%d %s\n", port, pr.Number, pr.Title)
	}
}

// WritePlainHTTP says the port's plain-HTTP links the edit switched to
// HTTPS, which answered there, and those still over plain HTTP, which
// MacPorts would have over HTTPS, with whether the https form answers.
func WritePlainHTTP(out io.Writer, switched, plain []engine.PlainURL) {
	if len(switched) > 0 {
		fmt.Fprintln(out, "Switched to HTTPS, which answers there:")
		for _, url := range switched {
			fmt.Fprintf(out, "  %s %s\n", url.Option, url.HTTPS)
		}
	}
	if len(plain) == 0 {
		return
	}
	fmt.Fprintln(out, "MacPorts prefers HTTPS; over plain HTTP:")
	for _, url := range plain {
		answer := url.HTTPS + " answers"
		if !url.Answers {
			answer = "https doesn't answer there"
		}
		fmt.Fprintf(out, "  %s %s: %s\n", url.Option, url.URL, answer)
	}
}

// serveWords are serve's state as a line: "serve: running (pid 34857) ·
// queue: 1 run".
func ServeWords(s engine.ServeState) string {
	line := "serve: not running"
	if s.Running {
		line = fmt.Sprintf("serve: running (pid %d)", s.PID)
		// A serve on another build than this command's is said.
		if build, upgraded := s.Upgraded(); upgraded {
			line += fmt.Sprintf(" · running dockhand %s; this command is %s", build, buildinfo.Current())
		}
		// A serve not acting on GitHub, as its login stands, says why.
		if s.NotActing != "" {
			line += " · not acting on GitHub: " + s.NotActing
		}
		// One that couldn't read its login says so, where it said nothing
		// and kept its state (the rc8 full stage's D-C3).
		if s.LoginProblem != "" {
			line += " · couldn't read its GitHub login: " + s.LoginProblem
		}
		if s.OpensPullRequests {
			line += " · opens PRs for passing updates"
		}
	}
	switch {
	case s.Queue == 0:
		return line + " · queue: empty"
	case s.Stopped > 0:
		return line + fmt.Sprintf(" · queue: %s, %d stopped", prose.Plural(s.Queue, "run"), s.Stopped)
	}
	return line + " · queue: " + prose.Plural(s.Queue, "run")
}

// batchExit is a batch's exit (the command-line UX review's §10): 1 where
// none of total went through, 3 where some stopped and need a look.
func BatchExit(failed []string, total int, done string) error {
	switch {
	case len(failed) > 0 && len(failed) == total:
		return &term.ExitError{Code: term.ExitFailure, Message: "nothing was " + done + ": " + strings.Join(failed, ", ") + " stopped"}
	case len(failed) > 0:
		return &term.ExitError{Code: term.ExitAttention, Message: strings.Join(failed, ", ") + " stopped; the rest went on"}
	}
	return nil
}

// writeUpstream reports what comparing the upstream archives found.
func WriteUpstream(out io.Writer, comparison *model.UpstreamComparison) {
	switch {
	case comparison == nil:
	case comparison.Problem != "":
		// What couldn't be checked holds as a finding does (D4), so it is
		// marked as one.
		fmt.Fprintf(out, "%s held: upstream archives not compared: %s\n", Look, comparison.Problem)
	case engine.NothingCompared(*comparison):
		fmt.Fprintf(out, "Upstream not compared: %s.\n", engine.CoverageWords(*comparison))
	case len(comparison.Changes) == 0:
		fmt.Fprintln(out, "Upstream source compared: no license, build file, or dependency changes.")
		if words := engine.CoverageWords(*comparison); words != "" {
			fmt.Fprintf(out, "    %s\n", words)
		}
	default:
		fmt.Fprintln(out, "Upstream changes:")
		held := false
		for _, change := range comparison.Changes {
			fmt.Fprintf(out, "  %s\n", UpstreamWords(engine.UnderUpstream(change)))
			held = held || change.Hold
		}
		if words := engine.CoverageWords(*comparison); words != "" {
			fmt.Fprintf(out, "    %s\n", words)
		}
		if held {
			fmt.Fprintf(out, "  %s.\n", prose.Sentence(engine.HoldsLetGo))
		}
	}
}

// UpstreamWords are a finding as update and status say it: one that holds
// says so in its word, "held", and one that doesn't carries no mark (part
// 5, item 2, where "!" meant it held and "·" that it didn't).
func UpstreamWords(change model.UpstreamChange) string {
	if change.Hold {
		return Look + " held: " + engine.WithRule(change.Message, change.Rule)
	}
	return Unmarked + " " + change.Message
}
