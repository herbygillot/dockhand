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
	"github.com/herbygillot/dockhand/internal/checks"
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
			fmt.Fprintf(streams.Out, "%s was queued, but Dockhand could not start a background worker (%v). Run dockhand serve to process it.\n", run.Name(), err)
			return nil
		}
		fmt.Fprintf(streams.Out, "%s is queued; a background worker (PID %d) will run it. Follow it with dockhand wait %s.\n", run.Name(), pid, run.Name())
		return nil
	case enqueue:
		fmt.Fprintf(streams.Out, "%s is queued; serve (PID %d) will run it. Follow it with dockhand wait %s.\n", run.Name(), leader.PID, run.Name())
		return nil
	}
	return Follow(ctx, e, session, run, streams, places, true, func(lead checks.Lead) {
		if quiet(streams) {
			// The notice is the redrawn line's, and -v's line (part 5,
			// item 4); off a terminal, the line, naming -d, since two
			// sessions wrapped their checks in screen not knowing it
			// (the bazel and exiv2 sessions' notes, 2026-10-24).
			switch {
			case streams.ErrTerminal() && lead.Here():
				streams.Status().Show(fmt.Sprintf("⋯ %s is running here; press Ctrl-C to stop it and keep completed results", run.Name()))
			case lead.Here() && !streams.Held():
				fmt.Fprintf(streams.Err, "%s is running here. Use check -d to run in the background.\n", run.Name())
			}
			return
		}
		if lead.Here() {
			fmt.Fprintf(streams.Err, "%s is running here because dockhand serve is not running. Press Ctrl-C to stop it; completed results will be saved. Use check -d to run in the background.\n", run.Name())
			return
		}
		fmt.Fprintf(streams.Err, "%s was handed to serve (PID %d). Press Ctrl-C to stop watching; the check will continue.\n", run.Name(), cmp.Or(lead.Serve, lead.Holder).PID)
	})
}

// quiet reports whether a check's progress is the redrawn line's alone, as
// at the default level and in a stage whose report is held back: its
// attempts, record IDs, and provider steps are -v's (part 5, item 4).
func quiet(streams term.Streams) bool {
	return streams.Held() || streams.Verbose() == 0 && !streams.JSON()
}

// Follow shows a run's progress until it ends, driving it here where
// nobody else sees it through, after before says who does, and reports its
// outcome; named is where the command named what it checks already.
// Wait follows a check until it ends and reports it, as dockhand wait does:
// one that ended is reported; with serve running, this observes; with no
// serve, it runs here, as a foreground check would.
func Wait(ctx context.Context, e *engine.Engine, run model.Run, streams term.Streams, places config.Places) error {
	if run.State.Terminal() {
		return Report(ctx, e, run, streams, places, false)
	}
	session, err := e.Session(ctx, model.SessionForeground)
	if err != nil {
		return err
	}
	defer session.End(context.WithoutCancel(ctx))
	return Follow(ctx, e, session, run, streams, places, false, func(lead checks.Lead) {
		if lead.Here() {
			fmt.Fprintf(streams.Err, "No dockhand serve is running, so %s will run here. Press Ctrl-C to stop it; completed results will be saved.\n", run.Name())
			return
		}
		fmt.Fprintf(streams.Err, "Following %s. Press Ctrl-C to stop watching; the check will continue.\n", run.Name())
	})
}

func Follow(ctx context.Context, e *engine.Engine, session *coord.Session, run model.Run, streams term.Streams, places config.Places, named bool, before func(checks.Lead)) error {
	journal, stop := startJournal(ctx, e, run, streams)
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
	journal.show(context.WithoutCancel(ctx))
	if err != nil {
		return err
	}
	return Report(context.WithoutCancel(ctx), e, run, streams, places, named)
}

// startJournal shows a run's progress as it's journaled, until the stop it
// returns is called.
func startJournal(ctx context.Context, e *engine.Engine, run model.Run, streams term.Streams) (*journal, func()) {
	journal := &journal{e: e, run: run.ID, out: streams.Err, verbose: streams.Verbose() > 0}
	if quiet(streams) {
		journal.held, journal.live = true, streams.ErrTerminal()
		journal.status = streams.Status()
		// Off a terminal, a foreground check's own redrawn line is
		// printed, a line each time it changes: rc5 printed nothing
		// between a check's plan and its report, which a session reading
		// dockhand's output took for a hang (the bazel session's notes,
		// 2026-10-24). A stage's held journal stays held.
		journal.steps = !journal.live && !streams.Held()
	}
	watching, cancel := context.WithCancel(ctx)
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
	return journal, func() {
		cancel()
		<-done
	}
}

// Bound is how long --for follows a check, as the person wrote it, which
// the words and the Next repeat.
type Bound struct {
	For  time.Duration
	Said string
}

// ParseBound reads --for's value: a duration in Go's forms, 9m, 90s,
// 1h30m, above zero; a bare number is refused for its missing unit.
func ParseBound(value string) (Bound, error) {
	if strings.Trim(value, "0123456789.") == "" {
		return Bound{}, fmt.Errorf("--for %s: name a duration with its unit, as 9m or 1h", value)
	}
	bound, err := time.ParseDuration(value)
	if err != nil {
		return Bound{}, fmt.Errorf("--for %s: name a duration with its unit, as 9m or 1h", value)
	}
	if bound <= 0 {
		return Bound{}, fmt.Errorf("--for %s: the bound is above zero", value)
	}
	return Bound{For: bound, Said: value}, nil
}

// WaitFor follows a check for at most bound, as dockhand wait --for does,
// and never drives it here: where nothing leads it, a background worker
// is started (drain) and followed. One that ended, or ends within the
// bound, is reported; one still queued or running at the bound is a
// StillRunning refusal, the check continuing.
func WaitFor(ctx context.Context, e *engine.Engine, run model.Run, streams term.Streams, places config.Places, bound Bound, drain func() (int, error)) error {
	if run.State.Terminal() {
		return Report(ctx, e, run, streams, places, false)
	}
	session, err := e.Session(ctx, model.SessionForeground)
	if err != nil {
		return err
	}
	defer session.End(context.WithoutCancel(ctx))
	lead, err := e.LeadOf(ctx, session, run.ID)
	if err != nil {
		return err
	}
	if lead.Here() {
		pid, err := drain()
		if err != nil {
			return refusal.New(refusal.Failed, "%s is queued and nothing runs it: Dockhand could not start a background worker (%v). Run dockhand serve, or dockhand wait %s without --for runs it here", run.Name(), err, run.Name())
		}
		fmt.Fprintf(streams.Err, "No dockhand serve is running; a background worker (PID %d) runs %s. Following it for %s.\n", pid, run.Name(), bound.Said)
	} else {
		fmt.Fprintf(streams.Err, "Following %s for %s.\n", run.Name(), bound.Said)
	}
	return FollowFor(ctx, e, run, streams, places, false, bound)
}

// QueuedFor follows a run check --for queued, as -d queues it, for at most
// bound: said as -d says who runs it, serve or a background worker drain
// starts, then followed (FollowFor). Where nothing leads and no worker
// starts, it's refused, the check left queued.
func QueuedFor(ctx context.Context, e *engine.Engine, run model.Run, streams term.Streams, places config.Places, bound Bound, drain func() (int, error)) error {
	if streams.JSON() {
		if result, err := checkResult(ctx, e, run); err == nil {
			streams.Emit(result)
		}
	}
	session, err := e.Session(ctx, model.SessionForeground)
	if err != nil {
		return err
	}
	defer session.End(context.WithoutCancel(ctx))
	leader, err := e.Leader(ctx, session)
	if err != nil {
		return err
	}
	if leader == nil {
		pid, err := drain()
		if err != nil {
			return refusal.New(refusal.Failed, "%s is queued and nothing runs it: Dockhand could not start a background worker (%v). Run dockhand serve, or dockhand wait %s without --for runs it here", run.Name(), err, run.Name())
		}
		fmt.Fprintf(streams.Out, "%s is queued; a background worker (PID %d) will run it.\n", run.Name(), pid)
	} else {
		fmt.Fprintf(streams.Out, "%s is queued; serve (PID %d) will run it.\n", run.Name(), leader.PID)
	}
	fmt.Fprintf(streams.Out, "Following it for %s.\n", bound.Said)
	return FollowFor(ctx, e, run, streams, places, true, bound)
}

// FollowFor follows a run that serve or a worker drives for at most bound,
// driving nothing, and reports it where it ends within the bound; one
// still queued or running then is a StillRunning refusal, in the words of
// the journal's latest progress or wait, with the wait that follows it
// further. Ctrl-C is Interrupted, as Follow's.
func FollowFor(ctx context.Context, e *engine.Engine, run model.Run, streams term.Streams, places config.Places, named bool, bound Bound) error {
	journal, stop := startJournal(ctx, e, run, streams)
	bounded, cancel := context.WithTimeout(ctx, bound.For)
	ended, err := e.Observe(bounded, run.ID)
	cancel()
	stop()
	journal.show(context.WithoutCancel(ctx))
	switch {
	case ctx.Err() != nil:
		return refusal.New(refusal.Interrupted, "stopped following %s; it continues", run.Name()).
			Then(refusal.Next{Verb: refusal.VerbWait, Args: []string{run.Name()}})
	case errors.Is(err, context.DeadlineExceeded):
		if ended, err = e.Run(ctx, run.ID); err != nil {
			return err
		}
		if !ended.State.Terminal() {
			return stillRunning(ended, journal, bound)
		}
	case err != nil:
		return err
	}
	return Report(context.WithoutCancel(ctx), e, ended, streams, places, named)
}

// stillRunning is a run still queued or running at a bound: "check-42 is
// still running after 9m: building socat on macOS 26; it continues", with
// dockhand wait check-42 --for 9m next.
func stillRunning(run model.Run, journal *journal, bound Bound) error {
	words := fmt.Sprintf("%s is still running after %s", run.Name(), bound.Said)
	if journal.latest != "" {
		words += ": " + journal.latest
	}
	if run.State == model.RunQueued {
		words = fmt.Sprintf("%s is still queued after %s", run.Name(), bound.Said)
		if journal.waited != "" {
			words += ", " + journal.waited
		}
	}
	return refusal.New(refusal.StillRunning, "%s; it continues", words).
		Then(refusal.Next{Verb: refusal.VerbWait, Args: []string{run.Name(), "--for", bound.Said}})
}

// journal prints a run's progress events as they are appended.
type journal struct {
	e    *engine.Engine
	run  model.RunID
	out  io.Writer
	last int64
	// held is a stage's journal, its report held back: its progress is
	// -v's, and the latest is the redrawn line where it's live. steps
	// prints, off a terminal, what the redrawn line would show, each
	// change once.
	held, live, steps bool
	// verbose shows the steps journaled at --verbose, as each archive a
	// guest is given.
	verbose bool
	status  *term.StatusLine
	shown   string
	// latest is the latest progress the run journaled, and waited its
	// latest wait, which a bounded follow's end says.
	latest, waited string
}

func (j *journal) show(ctx context.Context) {
	events, _ := j.e.RunEvents(ctx, j.run, j.last)
	for _, event := range events {
		j.last = event.Sequence
		switch event.Kind {
		case model.EventWaiting:
			j.waited = event.Message
		case model.EventProgress:
			j.latest = event.Message
		}
		if event.Level == model.LevelVerbose && !j.verbose {
			continue
		}
		switch event.Kind {
		case model.EventWaiting:
			// A wait is said at every level, where the redrawn line
			// can't show it: a silent wait reads as a hang (rc1's D-R2).
			if j.live {
				j.status.Show("⋯ " + event.Message)
			} else {
				fmt.Fprintf(j.out, "  %s\n", event.Message)
			}
		case model.EventProgress, model.EventTargetResult, model.EventExecutionRetry, model.EventExecutionState:
			switch {
			case j.live:
				j.status.Show("⋯ " + event.Message)
			case !j.held:
				fmt.Fprintf(j.out, "  %s\n", event.Message)
			case j.steps && event.Message != j.shown:
				j.shown = event.Message
				fmt.Fprintf(j.out, "  ⋯ %s\n", event.Message)
			}
		case model.EventArchiveAltered, model.EventArchiveGone:
			// A kept archive found altered, or gone, is marked, where it
			// was in the journal alone (the rc8 full stage's D-T1).
			fmt.Fprintf(j.out, "  ! %s\n", event.Message)
		}
	}
}

// Report prints a finished run's results and returns the exit its outcome
// calls for; named is where the command named what it checked already.
func Report(ctx context.Context, e *engine.Engine, run model.Run, streams term.Streams, places config.Places, named bool) error {
	if run.BaselineOf != "" {
		return reportBaseline(ctx, e, run, streams)
	}
	out := streams.Out
	evidence, err := e.RunEvidence(ctx, run.ID)
	if err != nil {
		return err
	}
	// What lint warned of, a new port's counted as ✗ (the docs dig's I13);
	// what its builds link and don't declare, a notice (v0.5.0 part 6), one
	// that couldn't be gathered saying nothing; and what it installed
	// against the port's previous build there (Then's item 9). Gathered
	// once, for the text and the JSON alike (Codex's review of 8e314b96,
	// finding 9).
	lint := engine.LintFindings(evidence)
	links, linksErr := e.UndeclaredLinks(ctx, run)
	installed, installedErr := e.InstallChanges(ctx, run, true)
	if streams.JSON() {
		result, err := checkResult(ctx, e, run)
		if err != nil {
			return err
		}
		result.Targets = view.EvidenceView(evidence)
		result.Lint = view.DetailLines(view.LintLines(lint))
		if linksErr == nil {
			result.Links = view.DetailLines(view.LinkLines(links))
		}
		if installedErr == nil {
			result.Installed = view.DetailLines(view.InstalledLines(installed))
		}
		streams.Emit(result)
	}
	fmt.Fprintln(out)
	view.WriteResults(out, "  ", evidence, false)
	if len(lint) > 0 {
		fmt.Fprintln(out)
		view.WriteLint(out, lint)
	}
	if linksErr == nil && len(links) > 0 {
		fmt.Fprintln(out)
		view.WriteLinks(out, links)
	}
	if installedErr == nil && len(installed) > 0 {
		fmt.Fprintln(out)
		view.WriteInstalled(out, installed)
	}
	revision, err := e.Revision(ctx, run.Revision)
	if err != nil {
		return err
	}
	// A check run with --keep names the shell that opens what it kept,
	// whose name the status line erased on a terminal (field testing's
	// curl +sectrust notes, note 5).
	var shells []refusal.Next
	if evidence.Plan.Keep {
		shells, _ = e.ShellSteps(ctx, run)
	}
	fmt.Fprintln(out)
	switch run.State {
	case model.RunPassed:
		// A check of files an earlier check already built finishes at
		// once: which check's results it took is said up front, so the
		// instant pass isn't a surprise (the Vx port's field testing).
		// What passed is named where the command didn't name it before
		// the check, as wait doesn't (part 5, item 3).
		passed := "Passed for " + checks.Describe(revision)
		if named {
			passed = "Passed"
		}
		if from := evidence.ReusedAll(); len(from) > 0 {
			if carried := engine.CarriedLines(evidence); len(carried) > 0 {
				fmt.Fprintf(out, "%s, using results from %s. No builds were needed: %s. Use --fresh to rebuild.\n", passed, prose.And(from), strings.Join(carried, ". "))
				return nil
			}
			fmt.Fprintf(out, "%s, using results from %s. No builds were needed because the inputs are unchanged; use --fresh to rebuild.\n", passed, prose.And(from))
			return nil
		}
		fmt.Fprintf(out, "%s.\n", passed)
		// A branch that changes nothing is an investigation, said as
		// status says it, with archive next, where status would have
		// said submit (field testing's curl +sectrust notes, note 1).
		if branch, err := e.Branch(ctx, run.Branch); err == nil {
			if status, err := e.BranchStatus(ctx, branch); err == nil {
				view.WriteInvestigated(out, status)
			}
		}
		for _, step := range view.RenderSteps(shells) {
			fmt.Fprintf(out, "Next: %s\n", step)
		}
		return nil
	case model.RunFailed:
		writeFailureLines(out, evidence)
		// A failure a standing decision accepts is named, as submit takes
		// it without a baseline.
		if standing, err := e.StandingAccepted(ctx, evidence); err == nil {
			for _, port := range slices.Sorted(maps.Keys(standing)) {
				fmt.Fprintf(out, "%s's failure is accepted (%s); it can be submitted.\n", port, standing[port].Words())
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
		return refusal.New(refusal.CheckFailed, "%s failed for %s: %s", run.Name(), checks.Describe(revision), run.Detail).
			// explain says where, the step's last lines, and whether a
			// baseline can say more (the command UX review's §7).
			Then(append([]refusal.Next{{Verb: refusal.VerbExplain, Args: []string{run.Name()}}}, shells...)...)
	case model.RunCanceled:
		return StoppedExit(run, evidence)
	}
	return refusal.New(refusal.Attention, "%s needs attention: %s", run.Name(), run.Detail)
}

// failureLines is how many of a failed step's last lines a check's
// report shows.
const failureLines = 15

// writeFailureLines shows, for each failure a reading of its log
// explained, that cause, and for each it didn't, the last lines its
// failed step printed.
func writeFailureLines(out io.Writer, evidence engine.Evidence) {
	for _, target := range evidence.Targets {
		for i := range target.Outcomes {
			// A dependency's failure is said with MacPorts' line naming the
			// dependency, which the log's last lines may not hold.
			if cell := target.Outcomes[i]; cell.Kind == engine.CellRecorded && cell.Outcome == model.OutcomeFailed && cell.Phase == model.PhaseDependencies {
				fmt.Fprintf(out, "%s: a dependency failed on %s: %s\n\n", target.Target.ID, checks.DescribeEnvironment(evidence.Plan.Environments[i]), cell.Detail)
				continue
			}
			where := ""
			if phase := target.Outcomes[i].Phase; phase != "" {
				where = " at " + string(phase)
			}
			if len(evidence.Plan.Environments) > 1 {
				where += " on " + checks.DescribeEnvironment(evidence.Plan.Environments[i])
			}
			// A cause a reading of its log found is said in one line,
			// where the row said only the phase (the openssl4 session's
			// note 6: sslscan's compiler error, found and unsaid).
			if cell := target.Outcomes[i]; cell.Kind == engine.CellRecorded && cell.Outcome == model.OutcomeFailed {
				if _, cause, found := strings.Cut(cell.Detail, checks.FromItsLog); found && cause != "" {
					fmt.Fprintf(out, "%s failed%s: %s\n\n", target.Target.ID, where, cause)
					continue
				}
			}
			lines := engine.FailureLines(evidence, target, i, failureLines)
			if len(lines) == 0 {
				continue
			}
			fmt.Fprintf(out, "%s failed%s. Last output:\n", target.Target.ID, where)
			for _, line := range lines {
				fmt.Fprintf(out, "    %s\n", line)
			}
			// A note read from the lines shown, never a hold (the abook
			// session, 2026-10-28).
			if note := buildlog.AutotoolsNote(lines); note != "" {
				fmt.Fprintf(out, "  Note: %s.\n", note)
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
		where := fmt.Sprintf("%s, attempt %d", checks.DescribeEnvironment(x.Environment), x.Attempt)
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
	// An earlier baseline a rebase left behind says why it no longer
	// answers (Then's item 4).
	for _, changed := range offer.Changed {
		fmt.Fprintln(out, changed+".")
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
	// What MacPorts' builders made of those ports on the releases they
	// failed on here, where they failed the same release, can save the
	// baseline's build.
	for _, words := range e.BuildersBeside(ctx, run, ports) {
		fmt.Fprintln(out, words+".")
	}
	fmt.Fprintf(out, "To see whether %s %s at master %s too: dockhand check --baseline --branch %s\n", strings.Join(ports, ", "), fails, base.Short(), branch.ShortName())
	return nil
}

// checkResult is a run's --json result, without its targets' results.
// Result is a check's object as wait and check --json give it before it
// ends: its branch, revision, plan, and run (checkResult).
func Result(ctx context.Context, e *engine.Engine, run model.Run) (view.CheckJSON, error) {
	return checkResult(ctx, e, run)
}

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
	if streams.JSON() {
		result, err := checkResult(ctx, e, run)
		if err != nil {
			return err
		}
		result.Targets = view.EvidenceView(base)
		streams.Emit(result)
	}
	fmt.Fprintln(out)
	WriteBaselineResults(out, base, branch)
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
func WriteBaselineResults(out io.Writer, base, branch engine.Evidence) {
	for _, row := range engine.BaselineComparison(base, branch) {
		fmt.Fprintf(out, "%s · %s\n", row.Target, checks.DescribeEnvironment(row.Environment))
		if row.Excluded != "" {
			fmt.Fprintf(out, "    not built at the base: %s\n", row.Excluded)
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
		return fmt.Sprintf("  not built at the base (%s)", base.Outcome)
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
