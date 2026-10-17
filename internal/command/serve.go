package command

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/buildinfo"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/model"
)

// servePoll is how often serve looks for work and a standby for the leader.
var servePoll = 2 * time.Second

// serveRefresh is how often serve reads your pull requests from GitHub.
var serveRefresh = engine.PullRequestRefresh

// serveGrace is how long serve's executable may be missing before serve
// takes dockhand as uninstalled.
var serveGrace = time.Minute

// serveCleanup is how often serve runs automatic cleanup (decision 36).
var serveCleanup = 24 * time.Hour

// serveNow is the clock serve's daily work reads; tests set it.
var serveNow = time.Now

func serveCommand(s *settings, streams Streams) *cobra.Command {
	var drain, install, uninstall, noNotify bool
	var shipTo string
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
(the config's maintainer), and carries them as far as serve.ship_to says, as
ship --to does: list counts them for status; commit prepares a branch with
one commit for each; check also checks each, on build.unattended's profile.

submit also opens a pull request for each branch serve prepared whose check
passed, at most serve.submit_limit a day, and never one with an upstream or
commit-rule finding, or one needing --accept: those wait on the attention
list. The passing branches you started are yours: it names them, for you
to ship. --ship-to sets serve.ship_to for one run.

serve.notify posts macOS notifications as checks finish and pull requests
change; --no-notify turns them off for one run. They are posted through
AppleScript, so macOS credits them to Script Editor, and clicking one opens
it.

--drain runs what is queued now and exits. --install makes serve a launchd
agent that starts at login and restarts if it stops, and runs it with the
flags given beside --install, such as --no-notify; --uninstall removes it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			switch shipTo {
			case "", "list", "commit", "check", "submit":
			default:
				return fmt.Errorf("invalid --ship-to value %s; choose list, commit, check, or submit", shipTo)
			}
			if install || uninstall {
				// The agent runs serve as this command line would.
				var flags []string
				if shipTo != "" {
					flags = append(flags, "--ship-to="+shipTo)
				}
				if noNotify {
					flags = append(flags, "--no-notify")
				}
				return serveAgent(ctx, s, streams, install, flags)
			}
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			session, err := e.Session(ctx, model.SessionServe)
			if err != nil {
				return err
			}
			defer session.End(context.WithoutCancel(ctx))
			err = e.Serve(ctx, session, serveOptions(e, s.file, streams.Out, drain, cmp.Or(shipTo, s.file.Serve.Stage()), s.file.Serve.Notifies() && !noNotify))
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
	cmd.Flags().StringVar(&shipTo, "ship-to", "", "override serve.ship_to for this run: list, commit, check, or submit")
	cmd.Flags().BoolVar(&noNotify, "no-notify", false, "disable macOS notifications for this run, overriding serve.notify")
	cmd.MarkFlagsMutuallyExclusive("install", "uninstall", "drain")
	return cmd
}

// serveIdentityEvery is how often serve reads who its login acts as.
var serveIdentityEvery = time.Minute

// serveIdentity is who serve's GitHub login acts as now, read afresh each
// time it's asked, from the same chain the engine's client reads. Tests
// stand another in.
var serveIdentity = func() func(context.Context) (string, error) {
	watcher := github.SystemIdentity(github.SystemCredentials{Store: authStore, Key: github.CredentialKey})
	return func(ctx context.Context) (string, error) {
		identity, err := watcher.Current(ctx)
		return identity.Account, err
	}
}

// servedExecutable is the file a long-lived serve watches for an upgrade;
// a drain ends by itself, and watches none. Tests stand another in.
var servedExecutable = func(drain bool) string {
	if drain {
		return ""
	}
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	return executable
}

// serveOptions turns the configuration and flags into what serve does,
// its lines going to out and its notices to macOS notifications when
// notify, serve.notify less --no-notify, allows; shipTo is serve.ship_to,
// or --ship-to.
func serveOptions(e *engine.Engine, file config.File, out io.Writer, drain bool, shipTo string, notify bool) engine.ServeOptions {
	capacity := map[string]int{}
	for name := range e.Providers {
		capacity[name] = file.Capacity(name)
	}
	hour, minute := file.Serve.Time()
	options := engine.ServeOptions{
		Drain:       drain,
		Capacity:    capacity,
		ShipTo:      shipTo,
		SubmitLimit: file.Serve.Limit(),
		Outdated: engine.ServeOutdated{Maintainers: file.Maintainers(), Hour: hour, Minute: minute,
			Tests: model.TestPolicy(file.Check.Tests)},
		Cleanup:         file.Cleanup.On(),
		CleanupAge:      file.Cleanup.Age(),
		Say:             func(line string) { fmt.Fprintln(out, line) },
		Poll:            servePoll,
		Refresh:         serveRefresh,
		CleanupEvery:    serveCleanup,
		MinFree:         file.Cleanup.Free(),
		Build:           buildinfo.Current().String(),
		Executable:      servedExecutable(drain),
		ExecutableGrace: serveGrace,
		Identity:        serveIdentity(),
		IdentityEvery:   serveIdentityEvery,
		Now:             serveNow,
	}
	if notify {
		options.Notify = func(title, text string) { _ = postNotification(title, text) }
	}
	return options
}

// postNotification shows a notification (macos.Notify); tests stand in
// for it.
var postNotification = func(title, text string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return macos.Notify(context.Background(), title, text)
}

// AgentLabel names serve's launchd agent.
const AgentLabel = "io.github.herbygillot.dockhand.serve"

// launchctl runs launchctl for serve's agent (macos.LaunchAgent), and
// returns what it wrote; tests stand in for it, nil being launchctl's own.
var launchctl func(ctx context.Context, args ...string) ([]byte, error)

// agentOS is the operating system serve --install targets; tests set it.
var agentOS = runtime.GOOS

func serveAgent(ctx context.Context, s *settings, streams Streams, install bool, flags []string) error {
	if agentOS != "darwin" {
		return errors.New("serve --install requires macOS; on other platforms, run dockhand serve with your service manager")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	plist := filepath.Join(home, "Library", "LaunchAgents", AgentLabel+".plist")
	// Both wait until launchd has let go of the agent, which bootout
	// returns before (the rc10 full stage's B7).
	agent := macos.LaunchAgent{Label: AgentLabel, Path: plist, Launchctl: launchctl}
	if !install {
		if err := agent.Remove(ctx); err != nil {
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
	options, _, _, err := s.options(ctx)
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
	// launchd starts the agent in /, so every path it's given is absolute.
	database, err := filepath.Abs(options.Database)
	if err != nil {
		return err
	}
	arguments := []string{executable, "serve", "--tree", tree, "--db", database}
	if options.Git != "" {
		arguments = append(arguments, "--git", options.Git)
	}
	environment, err := agentEnvironment()
	if err != nil {
		return err
	}
	logs := filepath.Join(filepath.Dir(database), "logs", "serve.log")
	if err := os.MkdirAll(filepath.Dir(logs), 0o755); err != nil {
		return err
	}
	if err := agent.Install(ctx, agentJob(append(arguments, flags...), environment, logs)); err != nil {
		return err
	}
	fmt.Fprintf(streams.Out, "serve now starts at login and restarts if it stops.\n  Agent  %s\n  Log    %s\n", config.Tilde(plist), config.Tilde(logs))
	var carried []string
	for _, variable := range environment[1:] {
		carried = append(carried, variable[0])
	}
	if len(carried) > 0 {
		fmt.Fprintf(streams.Out, "  With   %s, as set now\n", strings.Join(carried, ", "))
	}
	for _, token := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if os.Getenv(token) != "" {
			fmt.Fprintf(streams.Out, "%s is omitted from the agent file because that file is readable by other users on this Mac. serve uses the GitHub credentials saved in the Keychain; run dockhand setup github to save them.\n", token)
		}
	}
	fmt.Fprintln(streams.Out, "Run dockhand serve --install again after changing these settings. After an upgrade, serve restarts automatically with the new version.")
	return nil
}

// agentVariables are the settings dockhand reads from its environment that
// serve's agent needs as they were when it was installed:
//   - the configuration file;
//   - where master is fetched from;
//   - the port index's mirror and cache;
//   - Tart's home and dockhand's own;
//   - the SSH keys guests are reached with.
//
// A token is never written, since the agent's file is readable by anyone
// on the Mac.
var agentVariables = []string{config.PathVariable, "DOCKHAND_UPSTREAM", pullRequestsVariable, "DOCKHAND_INDEX_MIRROR", "DOCKHAND_INDEX_CACHE", "DOCKHAND_READING_CACHE", "DOCKHAND_TART_HOME", "TART_HOME", "DOCKHAND_SSH_DIR"}

// agentEnvironment is PATH and whichever of agentVariables are set, each a
// local path made absolute, since launchd starts the agent in /.
func agentEnvironment() ([][2]string, error) {
	environment := [][2]string{{"PATH", os.Getenv("PATH")}}
	for _, name := range agentVariables {
		value := os.Getenv(name)
		if value == "" {
			continue
		}
		if _, err := os.Stat(value); err == nil && !filepath.IsAbs(value) {
			if value, err = filepath.Abs(value); err != nil {
				return nil, err
			}
		}
		environment = append(environment, [2]string{name, value})
	}
	return environment, nil
}

// agentJob is the launchd job that runs serve for one ports checkout and
// database, kept alive in the background. launchd's PATH is minimal, so
// the installing shell's PATH is kept, for git and MacPorts.
func agentJob(arguments []string, environment [][2]string, log string) macos.LaunchdJob {
	variables := map[string]string{}
	for _, variable := range environment {
		variables[variable[0]] = variable[1]
	}
	// Kept alive while dockhand is installed, and not after port uninstall
	// removes it.
	return macos.LaunchdJob{Label: AgentLabel, Arguments: arguments, Log: log, Environment: variables, KeepAlive: true, KeepAliveWhile: arguments[0], ProcessType: "Background"}
}
