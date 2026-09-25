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
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// servePoll is how often serve looks for work and a standby for the leader.
var servePoll = 2 * time.Second

// serveRefresh is how often serve reads your pull requests from GitHub.
var serveRefresh = 5 * time.Minute

// serveCleanup is how often serve runs automatic cleanup (decision 36).
var serveCleanup = 24 * time.Hour

// serveNow is the clock serve's daily work reads; tests set it.
var serveNow = time.Now

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
			session, err := startSession(ctx, e, model.SessionServe)
			if err != nil {
				return err
			}
			defer session.End(context.WithoutCancel(ctx))
			err = e.Serve(ctx, session, serveOptions(e, s.file, streams.Out, drain, (s.file.Serve.SubmitPassing || submitPassing) && !noSubmitPassing))
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

// serveOptions turns the configuration and flags into what serve does,
// its lines going to out and its notices to macOS notifications when
// serve.notify allows.
func serveOptions(e *engine.Engine, file config.File, out io.Writer, drain, submitPassing bool) engine.ServeOptions {
	capacity := map[string]int{}
	for name := range e.Providers {
		capacity[name] = file.Capacity(name)
	}
	hour, minute := file.Serve.Time()
	options := engine.ServeOptions{
		Drain:         drain,
		Capacity:      capacity,
		SubmitPassing: submitPassing,
		SubmitLimit:   file.Serve.Limit(),
		Outdated: engine.ServeOutdated{Maintainers: file.Maintainers(), Hour: hour, Minute: minute, Mode: file.Serve.Mode(),
			On: file.Check.On, Tests: model.TestPolicy(file.Check.Tests)},
		Cleanup:      file.Cleanup.On(),
		CleanupAge:   file.Cleanup.Age(),
		Say:          func(line string) { fmt.Fprintln(out, line) },
		Poll:         servePoll,
		Refresh:      serveRefresh,
		CleanupEvery: serveCleanup,
		Now:          serveNow,
	}
	if file.Serve.Notifies() {
		options.Notify = func(title, text string) { _ = postNotification(title, text) }
	}
	return options
}

// postNotification shows a notification; tests stand in for it.
var postNotification = func(title, text string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	quote := func(value string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"` }
	return exec.Command("osascript", "-e", "display notification "+quote(text)+" with title "+quote("dockhand · "+title)).Run()
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
