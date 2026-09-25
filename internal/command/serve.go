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
	"time"

	"github.com/spf13/cobra"

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
	var drain, install, uninstall bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run queued checks, and keep running them",
		Long: `Runs the checks that are queued, people's first, and keeps running new
ones as they are queued. One serve leads; a second stands by and takes over
if the leader dies. Stopping serve leaves the check it was running for the
next serve, which picks it up where it stopped; finished results are kept.

serve only checks: it opens no pull requests. It also reads your open pull
requests every few minutes, so status shows their reviews and CI, and a
merged one marks its branch merged.

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
			err = serve(ctx, e, streams.Out, drain)
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
	cmd.MarkFlagsMutuallyExclusive("install", "uninstall", "drain")
	return cmd
}

func serve(ctx context.Context, e *engine.Engine, out io.Writer, drain bool) error {
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

	var providers []string
	for name := range e.Providers {
		providers = append(providers, name)
	}
	slices.Sort(providers)
	if len(providers) == 0 {
		providers = []string{"none set up; checks will need attention"}
	}
	fmt.Fprintf(out, "serve: leading (pid %d) · builds on %s · opens no pull requests; it only checks\n", os.Getpid(), strings.Join(providers, ", "))
	followed := &follower{e: e, out: out, reported: map[string]bool{}}
	for ctx.Err() == nil {
		if !drain {
			followed.maybe(ctx)
		}
		// A leader judged dead by a standby has lost the lease; it stops
		// rather than drive work twice.
		if err := session.Fenced(ctx, *lease, func(store.Tx) error { return nil }); err != nil {
			if errors.Is(err, store.ErrStale) {
				return errors.New("serve: another serve took over the lead; stopping")
			}
			return err
		}
		run, found, err := e.Next(ctx, session)
		if err != nil {
			return err
		}
		if !found {
			if drain {
				fmt.Fprintln(out, "serve: the queue is empty")
				return nil
			}
			select {
			case <-ctx.Done():
			case <-time.After(servePoll):
			}
			continue
		}
		branch, err := e.Branch(ctx, run.Branch)
		if err != nil {
			return err
		}
		verb := "running"
		if run.State == model.RunRunning {
			verb = "resuming"
		}
		fmt.Fprintf(out, "%s %s: %s\n", run.Name(), branch.ShortName(), verb)
		run, err = e.Resume(ctx, session, run.ID)
		if held := new(coord.HeldError); errors.As(err, &held) {
			continue
		}
		if err != nil {
			return err
		}
		if !run.State.Terminal() {
			fmt.Fprintf(out, "%s: left running for the next serve\n", run.Name())
			break
		}
		fmt.Fprintf(out, "%s %s: %s", run.Name(), branch.ShortName(), run.State)
		if run.Detail != "" && run.State != model.RunPassed {
			fmt.Fprintf(out, ": %s", run.Detail)
		}
		fmt.Fprintln(out)
	}
	fmt.Fprintln(out, "serve: stopped")
	return nil
}

// follower reads the pull requests every serveRefresh, reporting what
// changed, and each problem once until it changes.
type follower struct {
	e        *engine.Engine
	out      io.Writer
	last     time.Time
	reported map[string]bool
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
		}
	}
	f.reported = problems
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
