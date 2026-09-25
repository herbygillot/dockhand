package command

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/coord"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// servePoll is how often serve looks for work and a standby for the leader.
var servePoll = 2 * time.Second

// serveRefresh is how often serve reads your pull requests from GitHub.
var serveRefresh = 5 * time.Minute

func serveCommand(s *settings, streams Streams) *cobra.Command {
	var drain, install, uninstall, submitPassing, noSubmitPassing bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run queued checks, and keep running them",
		Long: `Runs the checks that are queued, people's first, and keeps running new
ones as they are queued. One serve leads; a second stands by and takes over
if the leader dies. Stopping serve leaves the check it was running for the
next serve, which picks it up where it stopped; finished results are kept.

serve checks, and by default opens no pull requests. It also reads your
open pull requests every few minutes, so status shows their reviews and CI,
and a merged one marks its branch merged, and it cleans up once a day.

Once a day, at serve.outdated_at, it looks for new releases of your ports
(the config's maintainer), as serve.for_outdated says: list counts them for
status; draft prepares a branch for each; check also checks each.

--submit-passing, or serve.submit_passing, also opens a pull request for
each branch serve prepared whose check passed, at most serve.submit_limit a
day, and never one with an upstream or commit-rule finding, or one needing
--accept: those wait on the attention list. --no-submit-passing turns it
off for one run. serve.notify posts macOS notifications as checks finish and
pull requests change.

--drain runs what is queued now and exits. --install makes serve a launchd
agent that starts at login and restarts if it stops; --uninstall removes it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if install || uninstall {
				return serveAgent(ctx, s, streams, install)
			}
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			options := serveOptions{file: s.file, submitPassing: (s.file.Serve.SubmitPassing || submitPassing) && !noSubmitPassing}
			err = serve(ctx, e, streams.Out, drain, options)
			// Being stopped is how serve ends, not a failure.
			if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
				fmt.Fprintln(streams.Out, "serve: stopped")
				return nil
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&drain, "drain", false, "run what is queued now, then exit")
	cmd.Flags().BoolVar(&install, "install", false, "install serve as a launchd agent that starts at login")
	cmd.Flags().BoolVar(&uninstall, "uninstall", false, "remove the launchd agent")
	cmd.Flags().BoolVar(&submitPassing, "submit-passing", false, "open pull requests for the updates serve prepared that pass (Design v3 §11's guardrails)")
	cmd.Flags().BoolVar(&noSubmitPassing, "no-submit-passing", false, "for this run, open none, whatever serve.submit_passing says")
	cmd.MarkFlagsMutuallyExclusive("install", "uninstall", "drain")
	cmd.MarkFlagsMutuallyExclusive("submit-passing", "no-submit-passing")
	return cmd
}

// serveOptions are serve's settings: the configuration file, and whether
// this run opens pull requests for passing updates.
type serveOptions struct {
	file          config.File
	submitPassing bool
}

func serve(ctx context.Context, e *engine.Engine, out io.Writer, drain bool, options serveOptions) error {
	session, err := startSession(ctx, e, model.SessionServe)
	if err != nil {
		return err
	}
	defer session.End(context.WithoutCancel(ctx))
	lease, err := lead(ctx, session, out, drain)
	if err != nil || lease == nil {
		return err
	}
	defer session.Release(context.WithoutCancel(ctx), *lease)

	out = &lockedWriter{w: out}
	capacity := map[string]int{}
	var providers []string
	for name := range e.Providers {
		providers = append(providers, name)
	}
	slices.Sort(providers)
	var described []string
	for _, name := range providers {
		capacity[name] = options.file.Capacity(name)
		described = append(described, fmt.Sprintf("%s (%d at a time)", name, capacity[name]))
	}
	if len(described) == 0 {
		described = []string{"none set up; checks will need attention"}
	}
	publishing := "opens no pull requests; it only checks"
	if options.submitPassing {
		publishing = fmt.Sprintf("opens PRs for passing updates it prepared, at most %d a day", options.file.Serve.Limit())
	}
	fmt.Fprintf(out, "serve: leading (pid %d) · builds on %s · %s\n", os.Getpid(), strings.Join(described, ", "), publishing)
	writeServing(e, options.submitPassing)
	notice := &notifier{on: options.file.Serve.Notifies()}
	followed := &follower{e: e, out: out, reported: map[string]bool{}, notice: notice}
	cleaned := &cleaner{e: e, out: out, settings: options.file.Cleanup}
	scanned := &outdatedScanner{e: e, out: out, file: options.file, notice: notice}
	submitter := &passingSubmitter{e: e, out: out, limit: options.file.Serve.Limit(), notice: notice}

	// Runs are driven concurrently, each provider up to its capacity. A
	// run takes a slot on every provider its plan builds on, for as long
	// as it runs; one that can't have them all waits, and the runs after
	// it that can go ahead.
	running, stop := context.WithCancel(ctx)
	defer stop()
	inUse := map[string]int{}
	inFlight := map[model.RunID]bool{}
	type finished struct {
		run       model.Run
		branch    model.Branch
		providers []string
		err       error
	}
	done := make(chan finished)
	var failure error
	launch := func(run model.Run, branch model.Branch, needs []string) {
		inFlight[run.ID] = true
		for _, name := range needs {
			inUse[name]++
		}
		verb := "running"
		if run.State == model.RunRunning {
			verb = "resuming"
		}
		fmt.Fprintf(out, "%s %s: %s\n", run.Name(), branch.ShortName(), verb)
		go func() {
			result, err := e.Resume(running, session, run.ID)
			if result.ID == "" {
				result = run
			}
			done <- finished{run: result, branch: branch, providers: needs, err: err}
		}()
	}
	settle := func(f finished) {
		delete(inFlight, f.run.ID)
		for _, name := range f.providers {
			inUse[name]--
		}
		if held := new(coord.HeldError); errors.As(f.err, &held) {
			return
		}
		if f.err != nil {
			if failure == nil && running.Err() == nil {
				failure = f.err
				stop()
			}
			return
		}
		if !f.run.State.Terminal() {
			fmt.Fprintf(out, "%s: left running for the next serve\n", f.run.Name())
			return
		}
		line := fmt.Sprintf("%s %s: %s", f.run.Name(), f.branch.ShortName(), f.run.State)
		if f.run.Detail != "" && f.run.State != model.RunPassed {
			line += ": " + f.run.Detail
		}
		fmt.Fprintln(out, line)
		notice.post(f.branch.ShortName(), line)
		// A check that passed may be what the submitter waits for.
		submitter.last = time.Time{}
	}
	for running.Err() == nil {
		if !drain {
			followed.maybe(running)
			scanned.maybe(running)
			if options.submitPassing {
				submitter.maybe(running)
			}
		}
		// A leader judged dead by a standby has lost the lease; it stops
		// rather than drive work twice.
		if err := session.Fenced(running, *lease, func(store.Tx) error { return nil }); err != nil {
			if errors.Is(err, store.ErrStale) {
				failure = errors.New("serve: another serve took over the lead; stopping")
			} else if running.Err() == nil {
				failure = err
			}
			break
		}
		candidates, err := e.Candidates(running, session)
		if err != nil {
			if running.Err() == nil {
				failure = err
			}
			break
		}
		waiting := false
		for _, run := range candidates {
			if inFlight[run.ID] {
				continue
			}
			needs, err := e.RunProviders(running, run)
			if err != nil {
				failure = err
				break
			}
			if slices.ContainsFunc(needs, func(name string) bool { return inUse[name] >= max(capacity[name], 1) }) {
				waiting = true
				continue
			}
			branch, err := e.Branch(running, run.Branch)
			if err != nil {
				failure = err
				break
			}
			launch(run, branch, needs)
		}
		if failure != nil {
			break
		}
		if len(inFlight) == 0 && !waiting {
			// Cleanup waits for a quiet moment, so it never delays a check.
			if drain {
				fmt.Fprintln(out, "serve: the queue is empty")
				return nil
			}
			cleaned.maybe(running)
		}
		select {
		case <-running.Done():
		case f := <-done:
			settle(f)
		case <-time.After(servePoll):
		}
	}
	// Runs still going stop with serve, and are left for the next one.
	stop()
	for len(inFlight) > 0 {
		settle(<-done)
	}
	if failure != nil {
		return failure
	}
	fmt.Fprintln(out, "serve: stopped")
	return nil
}

// lockedWriter lets serve's concurrent runs write their lines whole.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// follower reads the pull requests every serveRefresh, reporting what
// changed, and each problem once until it changes.
type follower struct {
	e        *engine.Engine
	out      io.Writer
	last     time.Time
	reported map[string]bool
	notice   *notifier
}

func (f *follower) maybe(ctx context.Context) {
	if !f.last.IsZero() && time.Since(f.last) < serveRefresh {
		return
	}
	f.last = time.Now()
	refreshed, err := f.e.RefreshPullRequests(ctx)
	problems := map[string]bool{}
	report := func(problem string) {
		problems[problem] = true
		if !f.reported[problem] {
			fmt.Fprintln(f.out, problem)
		}
	}
	if err != nil {
		report(fmt.Sprintf("serve: could not read pull requests: %v", err))
	}
	for _, r := range refreshed {
		if r.Err != nil {
			report(fmt.Sprintf("serve: could not read %s's #%d: %v", r.Branch.ShortName(), r.Branch.PullRequest.Number, r.Err))
		}
		for _, change := range r.Changes {
			fmt.Fprintf(f.out, "%s: %s\n", r.Branch.ShortName(), change)
			f.notice.post(r.Branch.ShortName(), change)
		}
	}
	f.reported = problems
}

// serveCleanup is how often serve runs automatic cleanup (decision 36).
var serveCleanup = 24 * time.Hour

// cleaner runs automatic cleanup at most once per serveCleanup for the
// database, whichever serve runs it: the last run is the time of a stamp
// file beside the database.
type cleaner struct {
	e        *engine.Engine
	out      io.Writer
	settings config.Cleanup
	failed   string
}

func (c *cleaner) maybe(ctx context.Context) {
	if !c.settings.On() {
		return
	}
	stamp := filepath.Join(filepath.Dir(c.e.LogDirectory()), "cleanup.stamp")
	if info, err := os.Stat(stamp); err == nil && time.Since(info.ModTime()) < serveCleanup {
		return
	}
	// The stamp goes first, so a cleanup that fails is not retried every
	// few seconds; it is tried again the next day, and its problem is
	// reported once until it changes.
	if err := os.WriteFile(stamp, nil, 0o644); err != nil {
		return
	}
	now := time.Now()
	_ = os.Chtimes(stamp, now, now)
	report, err := c.e.Cleanup(ctx, c.settings.Age())
	if err != nil {
		if problem := fmt.Sprintf("serve: cleanup: %v", err); problem != c.failed {
			fmt.Fprintln(c.out, problem)
			c.failed = problem
		}
	}
	for _, branch := range report.Branches {
		var removed []string
		for _, step := range branch.Steps {
			if step.Done {
				removed = append(removed, step.What)
			}
		}
		if len(removed) > 0 {
			fmt.Fprintf(c.out, "%s: cleaned up after the merge: removed %s\n", branch.Branch.ShortName(), strings.Join(removed, ", "))
		}
	}
	if len(report.Indexes) > 0 {
		fmt.Fprintf(c.out, "serve: removed %s unused for %s\n", plural(len(report.Indexes), "port index generation"), c.settings.Age())
	}
}

// lead takes the lead, or stands by until the leader goes. A drain with
// another serve leading has nothing to do.
func lead(ctx context.Context, session *coord.Session, out io.Writer, drain bool) (*model.Lease, error) {
	announced := false
	for {
		lease, err := session.Lead(ctx)
		if err == nil {
			return &lease, nil
		}
		held := new(coord.HeldError)
		if !errors.As(err, &held) {
			return nil, err
		}
		if drain {
			fmt.Fprintf(out, "serve (pid %d) leads and runs the queue; nothing to drain here\n", held.Holder.PID)
			return nil, nil
		}
		if !announced {
			fmt.Fprintf(out, "serve: standing by; serve (pid %d) leads\n", held.Holder.PID)
			announced = true
		}
		select {
		case <-ctx.Done():
			return nil, nil
		case <-time.After(servePoll):
		}
	}
}

// AgentLabel names serve's launchd agent.
const AgentLabel = "io.github.herbygillot.dockhand.serve"

// launchctl runs launchctl; tests stand in for it.
var launchctl = func(ctx context.Context, args ...string) error {
	out, err := exec.CommandContext(ctx, "launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %w: %s", strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return nil
}

// agentOS is the operating system serve --install targets; tests set it.
var agentOS = runtime.GOOS

func serveAgent(ctx context.Context, s *settings, streams Streams, install bool) error {
	if agentOS != "darwin" {
		return errors.New("serve --install makes a launchd agent, which is macOS's; elsewhere, run dockhand serve under your own service manager")
	}
	home, err := homeDir()
	if err != nil {
		return err
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", AgentLabel+".plist")
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	// Stopping an agent that is not loaded is not an error here.
	_ = launchctl(ctx, "bootout", domain+"/"+AgentLabel)
	if !install {
		if err := os.Remove(plist); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fmt.Fprintln(streams.Out, "Removed the serve agent; serve no longer starts at login.")
		return nil
	}
	e, err := s.open(ctx)
	if err != nil {
		return err
	}
	tree := e.Clone()
	options, _, _, err := s.options()
	e.Close()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if executable, err = filepath.EvalSymlinks(executable); err != nil {
		return err
	}
	logs := filepath.Join(filepath.Dir(options.Database), "logs", "serve.log")
	data, err := agentPlist(executable, tree, options.Database, os.Getenv("PATH"), logs)
	if err != nil {
		return err
	}
	for _, dir := range []string{filepath.Dir(plist), filepath.Dir(logs)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(plist, data, 0o644); err != nil {
		return err
	}
	if err := launchctl(ctx, "bootstrap", domain, plist); err != nil {
		return err
	}
	fmt.Fprintf(streams.Out, "serve now starts at login and restarts if it stops.\n  Agent  %s\n  Log    %s\nAfter upgrading dockhand, run serve --install again to restart it on the new build.\n", tilde(plist), tilde(logs))
	return nil
}

// agentPlist is the launchd property list that runs serve for one ports
// checkout and database. launchd's PATH is minimal, so the installing
// shell's PATH is kept, for git and MacPorts.
func agentPlist(executable, tree, database, path, log string) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	key := func(name string) { fmt.Fprintf(&b, "  <key>%s</key>\n", name) }
	str := func(indent, value string) error {
		b.WriteString(indent + "<string>")
		if err := xml.EscapeText(&b, []byte(value)); err != nil {
			return err
		}
		b.WriteString("</string>\n")
		return nil
	}
	key("Label")
	if err := str("  ", AgentLabel); err != nil {
		return nil, err
	}
	key("ProgramArguments")
	b.WriteString("  <array>\n")
	for _, arg := range []string{executable, "serve", "--tree", tree, "--db", database} {
		if err := str("    ", arg); err != nil {
			return nil, err
		}
	}
	b.WriteString("  </array>\n")
	key("EnvironmentVariables")
	b.WriteString("  <dict>\n    <key>PATH</key>\n")
	if err := str("    ", path); err != nil {
		return nil, err
	}
	b.WriteString("  </dict>\n")
	key("RunAtLoad")
	b.WriteString("  <true/>\n")
	key("KeepAlive")
	b.WriteString("  <true/>\n")
	key("ProcessType")
	b.WriteString("  <string>Background</string>\n")
	for _, name := range []string{"StandardOutPath", "StandardErrorPath"} {
		key(name)
		if err := str("  ", log); err != nil {
			return nil, err
		}
	}
	b.WriteString("</dict>\n</plist>\n")
	return b.Bytes(), nil
}
