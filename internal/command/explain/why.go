package explain

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/buildlog"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
)

// stepLines are how many of a failed step's last lines explain shows.
const stepLines = 8

// explainCheck says why a check is in its state (the command UX review's
// §7): where it failed, the failed step's last lines, the test failures
// the policy let pass, and whether a baseline can say more; or what it's
// doing, or where it passed.
func explainCheck(ctx context.Context, e *engine.Engine, out io.Writer, run model.Run) error {
	branch, err := e.Branch(ctx, run.Branch)
	if err != nil {
		return err
	}
	revision, err := e.Revision(ctx, run.Revision)
	if err != nil {
		return err
	}
	// The check is named where it's asked about, so the heading names its
	// branch, and the commands below name the check.
	heading := fmt.Sprintf("%s's check, of %s", branch.ShortName(), engine.Describe(revision))
	if run.BaselineOf != "" {
		heading = fmt.Sprintf("%s's baseline, at master", branch.ShortName())
	}
	switch run.State {
	case model.RunQueued, model.RunRunning:
		fmt.Fprintf(out, "%s: %s\nNext: dockhand wait %s\n", heading, run.State, run.Name())
		return nil
	case model.RunCanceled:
		fmt.Fprintf(out, "%s: stopped before completion%s\nNext: dockhand retry %s\n", heading, detail(run.Detail), run.Name())
		return nil
	case model.RunAttention:
		fmt.Fprintf(out, "%s: needs attention%s\nNext: dockhand logs %s\n", heading, detail(run.Detail), run.Name())
		return nil
	}
	evidence, err := e.RunEvidence(ctx, run.ID)
	if err != nil {
		return err
	}
	if run.State == model.RunPassed {
		fmt.Fprintf(out, "%s: %s\n", heading, view.PassedOn(evidence))
		writeAdvisory(out, evidence)
		return nil
	}
	fmt.Fprintf(out, "%s: failed%s\n", heading, detail(run.Detail))
	logs, err := e.Logs(ctx, run.ID)
	if err != nil {
		return err
	}
	next := "dockhand logs " + run.Name()
	for _, target := range evidence.Failed() {
		port := target.Target.Target.Name
		for i, cell := range target.Outcomes {
			if cell.Outcome != model.OutcomeFailed {
				continue
			}
			fmt.Fprintf(out, "  %s %s on %s: failed at %s%s\n", view.Broken, port, engine.DescribeEnvironment(evidence.Plan.Environments[i]), cell.Phase, detail(cell.Detail))
		}
		next = fmt.Sprintf("dockhand logs %s %s", run.Name(), port)
		writeStep(out, logs, run, port)
	}
	writeAdvisory(out, evidence)
	if offer, err := e.BaselineCandidates(ctx, run); err == nil && len(offer.Ports) > 0 {
		fmt.Fprintf(out, "To check whether %s also %s on master %s, run: dockhand check --baseline --branch %s\n", prose.And(offer.Ports), map[bool]string{true: "fails", false: "fail"}[len(offer.Ports) == 1], offer.Base.Short(), branch.ShortName())
	}
	fmt.Fprintf(out, "Next: %s\n", next)
	return nil
}

// detail is a run's or a result's own words after what it says, where it
// has any.
func detail(words string) string {
	if words = strings.TrimSpace(words); words == "" {
		return ""
	}
	return ": " + strings.ReplaceAll(words, "\n", "; ")
}

// writeStep is the last lines of the step a port's build failed at, from
// the log shown first for it, where its provider recorded where its steps
// begin.
func writeStep(out io.Writer, logs engine.RunLogs, run model.Run, port string) {
	chosen, _, ok := logs.ForPort(port)
	if !ok || chosen.Result.Phase == "" {
		return
	}
	step := model.Step(chosen.Result.Phase)
	from, to, ok := logs.StepLines(chosen, step)
	if !ok {
		return
	}
	data, err := buildlog.ReadFile(chosen.Result.Log)
	if err != nil {
		return
	}
	lines, ok := buildlog.Between(data, from, to)
	if !ok {
		return
	}
	last := prose.LastLines(string(lines), stepLines)
	if len(last) == 0 {
		return
	}
	fmt.Fprintf(out, "    its %s step ended:\n", step)
	for _, line := range last {
		fmt.Fprintf(out, "      %s\n", line)
	}
	if note := buildlog.AutotoolsNote(last); note != "" {
		fmt.Fprintf(out, "    Note: %s.\n", note)
	}
	fmt.Fprintf(out, "    dockhand logs %s %s --phase %s shows the step\n", run.Name(), port, step)
}

// writeAdvisory says the ports whose tests failed where the policy let
// them pass.
func writeAdvisory(out io.Writer, evidence engine.Evidence) {
	for _, target := range evidence.Targets {
		if !target.Passed {
			continue
		}
		for i, cell := range target.Outcomes {
			if cell.Outcome == model.OutcomePassed && cell.Tests.Failed() {
				fmt.Fprintf(out, "  %s %s on %s: built; tests failed, but the advisory test policy allows the check to pass\n", view.Look, target.Target.Target.Name, engine.DescribeEnvironment(evidence.Plan.Environments[i]))
			}
		}
	}
}

// explainBranch says why a branch is where it is: what needs you, with
// what holds it and what clears it, or what it's doing, or what it waits
// on.
func explainBranch(ctx context.Context, e *engine.Engine, out io.Writer, branch model.Branch) error {
	status, err := e.BranchStatus(ctx, branch)
	if err != nil {
		return err
	}
	ctx, session, end, err := e.Observing(ctx)
	if err != nil {
		return err
	}
	defer end()
	statuses := []engine.BranchStatus{status}
	if err := e.JudgeStopped(ctx, session, statuses); err != nil {
		return err
	}
	status = statuses[0]
	facts := status.Facts()
	name := branch.ShortName()
	switch branchstate.BucketOf(facts) {
	case branchstate.BucketNeedsYou:
		fmt.Fprintf(out, "%s needs attention\n", name)
		viewed := view.AttentionFor(status)
		for i, row := range branchstate.Attend(facts) {
			fmt.Fprintf(out, "  %s %s  %s\n", viewed[i].Kind.Mark(), name, viewed[i].What)
			for _, hold := range engine.HoldLines(row.Holds) {
				fmt.Fprintf(out, "    %s\n", hold)
			}
			fmt.Fprintf(out, "Next: %s\n", view.RenderNext(row.Next, row.Reason))
		}
	case branchstate.BucketWorking:
		fmt.Fprintf(out, "%s is in progress: %s; no action needed yet\n", name, workingWords(status))
		if next, ok := branchstate.Working(facts); ok {
			fmt.Fprintf(out, "Next: %s\n", view.RenderNext(next, ""))
		}
	case branchstate.BucketWaiting:
		fmt.Fprintf(out, "%s is waiting for review of #%d\n", name, branch.PullRequest.Number)
	default:
		fmt.Fprintf(out, "%s: %s\n", name, branch.State)
	}
	return nil
}

// workingWords is what a branch under way is doing: its check's state, or
// that nothing checked it yet.
func workingWords(status engine.BranchStatus) string {
	if check := view.CheckState(status); check != "—" {
		return "its check is " + check
	}
	if status.Commits == 0 && len(status.Edited) == 0 {
		return "nothing is edited yet"
	}
	return "nothing has checked it yet"
}
