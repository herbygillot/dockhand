package checkrun

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildlog"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/term"
)

// RunQueued sees a queued run through (Design v3 §11): handed to serve and
// followed when serve leads, otherwise run here; with enqueue, only
// queued, saying whether anything will run it, by a background drain
// that drain starts where nothing leads.
func RunQueued(ctx context.Context, e *engine.Engine, run model.Run, streams term.Streams, enqueue bool, places config.Places, drain func() (int, error)) error {
	session, err := e.Session(ctx, model.SessionForeground)
	if err != nil {
		return err
	}
	defer session.End(context.WithoutCancel(ctx))
	leader, err := e.Leader(ctx, session)
	if err != nil {
		return err
	}
	if enqueue && streams.JSON() {
		result, err := checkResult(ctx, e, run)
		if err != nil {
			return err
		}
		streams.Emit(result)
	}
	switch {
	case enqueue && leader == nil:
		// Nothing leads, so a drain of its own runs it, outliving this
		// command as automatic cleanup does: an agent's check outlived
		// its tool's timeout, and a session's end killed one run here.
		pid, err := drain()
		if err != nil {
			fmt.Fprintf(streams.Out, "%s queued, but a background drain couldn't start (%v). Start dockhand serve to run it.\n", run.Name(), err)
			return nil
		}
		fmt.Fprintf(streams.Out, "%s queued; a background drain (pid %d) runs it. dockhand wait %s follows it.\n", run.Name(), pid, run.Name())
		return nil
	case enqueue:
		fmt.Fprintf(streams.Out, "%s queued; serve (pid %d) runs it. dockhand wait %s follows it.\n", run.Name(), leader.PID, run.Name())
		return nil
	}
	return Follow(ctx, e, session, run, streams, places, func(lead engine.Lead) {
		if lead.Here() {
			fmt.Fprintf(streams.Err, "%s runs here, since no dockhand serve is running. Ctrl-C stops it and keeps what finished.\n", run.Name())
			return
		}
		fmt.Fprintf(streams.Err, "%s handed to serve (pid %d); following it. Ctrl-C stops following, not the check.\n", run.Name(), cmp.Or(lead.Serve, lead.Holder).PID)
	})
}

// Follow shows a run's progress until it ends, driving it here where
// nobody else sees it through, after before says who does, and reports its
// outcome.
func Follow(ctx context.Context, e *engine.Engine, session *coord.Session, run model.Run, streams term.Streams, places config.Places, before func(engine.Lead)) error {
	journal := &journal{e: e, run: run.ID, out: streams.Err}
	watching, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			journal.show(watching)
			select {
			case <-watching.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
	}()
	// A serve that started meanwhile may have taken the run; then it is
	// followed instead, and said.
	run, taker, err := e.SeeThrough(ctx, session, run.ID, before)
	if taker != nil {
		fmt.Fprintf(streams.Err, "%s was taken by serve (pid %d); following it.\n", run.Name(), taker.PID)
	}
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		err = refusal.New(refusal.Interrupted, "stopped following %s; it continues", run.Name()).
			Then(refusal.Next{Verb: refusal.VerbWait, Args: []string{run.Name()}})
	}
	stop()
	<-done
	journal.show(context.WithoutCancel(ctx))
	if err != nil {
		return err
	}
	return Report(context.WithoutCancel(ctx), e, run, streams, places)
}

// journal prints a run's progress events as they are appended.
type journal struct {
	e    *engine.Engine
	run  model.RunID
	out  io.Writer
	last int64
}

func (j *journal) show(ctx context.Context) {
	events, _ := j.e.RunEvents(ctx, j.run, j.last)
	for _, event := range events {
		j.last = event.Sequence
		switch event.Kind {
		case model.EventProgress, model.EventTargetResult, model.EventExecutionRetry, model.EventExecutionState:
			fmt.Fprintf(j.out, "  %s\n", event.Message)
		case model.EventArchiveAltered, model.EventArchiveGone:
			// A kept archive found altered, or gone, is marked, where it
			// was in the journal alone (the rc8 full stage's D-T1).
			fmt.Fprintf(j.out, "  ! %s\n", event.Message)
		}
	}
}

// Report prints a finished run's results and returns the exit its outcome
// calls for.
func Report(ctx context.Context, e *engine.Engine, run model.Run, streams term.Streams, places config.Places) error {
	if run.BaselineOf != "" {
		return reportBaseline(ctx, e, run, streams)
	}
	out := streams.Out
	evidence, err := e.RunEvidence(ctx, run.ID)
	if err != nil {
		return err
	}
	if streams.JSON() {
		result, err := checkResult(ctx, e, run)
		if err != nil {
			return err
		}
		result.Targets = view.EvidenceView(evidence)
		streams.Emit(result)
	}
	fmt.Fprintln(out)
	view.WriteResults(out, "  ", evidence, false)
	revision, err := e.Revision(ctx, run.Revision)
	if err != nil {
		return err
	}
	fmt.Fprintln(out)
	switch run.State {
	case model.RunPassed:
		// A check of files an earlier check already built finishes at
		// once: which check's results it took is said up front, so the
		// instant pass isn't a surprise (the Vx port's field testing).
		if from := evidence.ReusedAll(); len(from) > 0 {
			fmt.Fprintf(out, "Passed for %s, reusing %s's results: the same inputs, so nothing was built (--fresh builds again).\n", engine.Describe(revision), prose.And(from))
			return nil
		}
		fmt.Fprintf(out, "Passed for %s.\n", engine.Describe(revision))
		return nil
	case model.RunFailed:
		writeFailureLines(out, evidence)
		// A failure a standing decision accepts is named, as submit takes
		// it without a baseline.
		if standing, err := e.StandingAccepted(ctx, evidence); err == nil {
			for _, port := range slices.Sorted(maps.Keys(standing)) {
				fmt.Fprintf(out, "%s's failure is accepted, %s; submit takes it.\n", port, standing[port].Words())
			}
			if len(standing) > 0 {
				fmt.Fprintln(out)
			}
		}
		if logs, err := e.Logs(ctx, run.ID); err == nil {
			writeFailureLogs(out, places, logs)
		}
		if err := hintBaseline(ctx, e, out, run); err != nil {
			fmt.Fprintf(streams.Err, "baseline: %v\n", err)
		}
		return refusal.New(refusal.CheckFailed, "%s failed for %s: %s", run.Name(), engine.Describe(revision), run.Detail).
			Then(refusal.Next{Verb: refusal.VerbLogs, Args: []string{run.Name()}})
	case model.RunCanceled:
		return StoppedExit(run, evidence)
	}
	return refusal.New(refusal.Attention, "%s needs attention: %s", run.Name(), run.Detail)
}

// failureLines is how many of a failed step's last lines a check's
// report shows.
const failureLines = 15

// writeFailureLines shows, for each failure no reading of its log
// explained, the last lines its failed step printed.
func writeFailureLines(out io.Writer, evidence engine.Evidence) {
	for _, target := range evidence.Targets {
		for i := range target.Outcomes {
			// A dependency's failure is said with MacPorts' line naming the
			// dependency, which the log's last lines may not hold.
			if cell := target.Outcomes[i]; cell.Kind == engine.CellRecorded && cell.Outcome == model.OutcomeFailed && cell.Phase == model.PhaseDependencies {
				fmt.Fprintf(out, "%s: a dependency failed on %s: %s\n\n", target.Target.ID, engine.DescribeEnvironment(evidence.Plan.Environments[i]), cell.Detail)
				continue
			}
			lines := engine.FailureLines(evidence, target, i, failureLines)
			if len(lines) == 0 {
				continue
			}
			where := ""
			if phase := target.Outcomes[i].Phase; phase != "" {
				where = " at " + string(phase)
			}
			if len(evidence.Plan.Environments) > 1 {
				where += " on " + engine.DescribeEnvironment(evidence.Plan.Environments[i])
			}
			fmt.Fprintf(out, "%s failed%s; the last it printed:\n", target.Target.ID, where)
			for _, line := range lines {
				fmt.Fprintf(out, "    %s\n", line)
			}
			fmt.Fprintln(out)
		}
	}
}

// StoppedExit says what a check that was stopped leaves: what it finished,
// or nothing, when it finished nothing, as one cancelled while queued.
func StoppedExit(run model.Run, evidence engine.Evidence) error {
	if !evidence.Recorded() {
		return refusal.New(refusal.Interrupted, "%s stopped before anything finished", run.Name())
	}
	return refusal.New(refusal.Interrupted, "%s stopped; finished results are kept", run.Name())
}

// StopWords say what cancelling a check did: one only queued is cancelled
// before it started, and one running keeps what it finished.
func StopWords(run model.Run, wasQueued bool) string {
	if wasQueued {
		return fmt.Sprintf("Canceled %s before it started.", run.Name())
	}
	return fmt.Sprintf("Stopped %s; what it finished is kept.", run.Name())
}

// writeFailureLogs lists where each failure's log is, by environment and
// attempt, .gz once compressed, and why an attempt was tried again, where
// a failed check said only "dockhand logs check-N" (the exiv2 field notes,
// 2026-10-19).
func writeFailureLogs(out io.Writer, places config.Places, logs engine.RunLogs) {
	var lines []string
	for _, execution := range logs.Executions {
		x := execution.Execution
		where := fmt.Sprintf("%s, attempt %d", engine.DescribeEnvironment(x.Environment), x.Attempt)
		if x.State == model.ExecutionInfrastructure && x.Detail != "" {
			lines = append(lines, fmt.Sprintf("  %s was tried again: %s", where, x.Detail))
		}
		for _, result := range execution.Results {
			if result.Log != "" && (result.Outcome == model.OutcomeFailed || result.Outcome == model.OutcomeInterrupted || result.Tests.Failed()) {
				lines = append(lines, fmt.Sprintf("  %s on %s: %s", result.Target, where, places.Tilde(buildlog.Where(result.Log))))
			}
		}
	}
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(out, "Logs:")
	for _, line := range lines {
		fmt.Fprintln(out, line)
	}
	fmt.Fprintln(out)
}

// hintBaseline points a failed check to check --baseline when a baseline
// can say something about what failed: whether master fails the same way
// (Design v3 §6.8). That is a port master has that failed at install or
// test; a lint, fetch, or checksum failure is the branch's own.
func hintBaseline(ctx context.Context, e *engine.Engine, out io.Writer, run model.Run) error {
	offer, err := e.BaselineCandidates(ctx, run)
	if err != nil {
		return err
	}
	// A port whose tests are new here is said, where a baseline would
	// compare tests master doesn't run (the exiv2 field notes, 2026-10-19).
	if len(offer.NewTests) > 0 {
		fmt.Fprintln(out, engine.NewTestsWords(offer.NewTests)+".")
	}
	ports, base := offer.Ports, offer.Base
	if len(ports) == 0 {
		return nil
	}
	branch, err := e.Branch(ctx, run.Branch)
	if err != nil {
		return err
	}
	fails := "fails"
	if len(ports) > 1 {
		fails = "fail"
	}
	fmt.Fprintf(out, "To see whether %s %s at master %s too: dockhand check --baseline --branch %s\n", strings.Join(ports, ", "), fails, base.Short(), branch.ShortName())
	return nil
}

// checkResult is a run's --json result, without its targets' results.
func checkResult(ctx context.Context, e *engine.Engine, run model.Run) (view.CheckJSON, error) {
	result := view.CheckJSON{Targets: []view.TargetJSON{}}
	summary, err := e.CheckSummary(ctx, run)
	if err != nil {
		return result, err
	}
	revisionResult, planResult, runResult := view.RevisionView(summary.Revision), view.PlanView(summary.Plan), view.RunView(run)
	result.Branch, result.Revision, result.Plan, result.Run = summary.Branch.ShortName(), &revisionResult, &planResult, &runResult
	result.Altered = summary.Altered
	return result, nil
}

// reportBaseline sets each port's result at the base beside its result in
// the check the baseline looks into. It says what happened in each run and
// nothing more: a baseline never establishes a cause, and never fails.
func reportBaseline(ctx context.Context, e *engine.Engine, run model.Run, streams term.Streams) error {
	out := streams.Out
	base, err := e.RunEvidence(ctx, run.ID)
	if err != nil {
		return err
	}
	branch, err := e.RunEvidence(ctx, run.BaselineOf)
	if err != nil {
		return err
	}
	revision, err := e.Revision(ctx, run.Revision)
	if err != nil {
		return err
	}
	if streams.JSON() {
		result, err := checkResult(ctx, e, run)
		if err != nil {
			return err
		}
		result.Targets = view.EvidenceView(base)
		streams.Emit(result)
	}
	fmt.Fprintln(out)
	WriteBaselineResults(out, base, branch, revision.Source.Commit.Short())
	switch run.State {
	case model.RunCanceled:
		return StoppedExit(run, base)
	case model.RunAttention:
		return refusal.New(refusal.Attention, "%s needs attention: %s", run.Name(), run.Detail)
	}
	return nil
}

// WriteBaselineResults sets each port's result at the base beside the
// branch's, in each environment where the baseline built it, and where
// the check failed it but master doesn't build it. A baseline rebuilds a
// port only where it failed, so elsewhere there is nothing to set beside.
func WriteBaselineResults(out io.Writer, base, branch engine.Evidence, master string) {
	for _, row := range engine.BaselineComparison(base, branch) {
		fmt.Fprintf(out, "%s at master %s · %s\n", row.Target, master, engine.DescribeEnvironment(row.Environment))
		if row.Excluded != "" {
			fmt.Fprintf(out, "  · not built at the base: %s\n", row.Excluded)
			continue
		}
		fmt.Fprintf(out, "  %s\n", BaselineWords(row.Base, row.Branch, branch.Run.Name()))
	}
}

// baselineTestWords sets a port's tests at the base beside its tests on
// the branch, where either failed though the policy let the port pass: what
// a baseline run for advisory test failures is for, as uvw's failed at the
// base too, and uvw2's passed there.
func baselineTestWords(base, branch model.TestOutcome, check string) string {
	verb := func(tests model.TestOutcome) string {
		if tests == model.TestsTimedOut {
			return "time out"
		}
		return "fail"
	}
	switch {
	case base.Failed() && branch.Failed():
		return fmt.Sprintf("tests %s at the base and in %s; the failure also occurs without this branch's changes.", verb(base), check)
	case branch.Failed() && base == model.TestsPassed:
		return fmt.Sprintf("its tests pass at the base, and %s in %s; the cause isn't established.", verb(branch), check)
	case branch.Failed():
		return fmt.Sprintf("its tests %s in %s, and weren't run at the base (%s), so there's nothing to set beside them.", verb(branch), check, base)
	case branch == model.TestsPassed:
		return fmt.Sprintf("its tests %s at the base, and pass in %s.", verb(base), check)
	}
	return fmt.Sprintf("its tests %s at the base, and weren't run in %s (%s).", verb(base), check, branch)
}

func BaselineWords(base, branch model.TargetResult, check string) string {
	passed := func(r model.TargetResult) bool { return r.Outcome == model.OutcomePassed }
	switch {
	case base.Outcome != model.OutcomePassed && base.Outcome != model.OutcomeFailed:
		return fmt.Sprintf("· not built at the base (%s)", base.Outcome)
	case passed(base) && passed(branch) && (base.Tests.Failed() || branch.Tests.Failed()):
		return "✓ builds at the base, as it does on the branch; " + baselineTestWords(base.Tests, branch.Tests, check)
	case passed(base) && passed(branch):
		return "✓ builds at the base, as it does on the branch."
	case passed(base):
		return fmt.Sprintf("✓ builds at the base. This branch's result differs (%s); the cause isn't established.", check)
	case passed(branch):
		return fmt.Sprintf("✗ fails at the base, at %s, and builds on the branch (%s).", base.Phase, check)
	}
	return fmt.Sprintf("✗ fails at the base too, at %s. Both results are kept; the cause isn't established.", base.Phase)
}
