package serve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/macos"
	"github.com/herbygillot/dockhand/internal/term"
)

func TestServeDrainsTheQueue(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	_, _, err := cmdtest.Dockhand(t, "check", "-d")
	require.NoError(t, err)
	out, _, err := cmdtest.Dockhand(t, "serve", "--drain")
	require.NoError(t, err)
	require.Contains(t, out, "serve: leading (pid ")
	require.Contains(t, out, "builds on command (1 check at a time), github (2 checks at a time) · opens no pull requests; it only checks\n")
	require.Contains(t, out, "check-1 jq-update: running\ncheck-1 jq-update: passed\nserve: the queue is empty\n")
	out, _, err = cmdtest.Dockhand(t, "status", "--all")
	require.NoError(t, err)
	require.NotContains(t, out, "Checks queued and running")
	require.Contains(t, out, "\nserve: not running · queue: empty\n")
}

func TestACheckIsHandedToServe(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	ctx, stop := context.WithCancel(t.Context())
	var served cmdtest.SyncBuffer
	done := make(chan error)
	go func() {
		done <- cmdtest.RunFor(t, ctx, []string{"serve"}, term.Streams{In: strings.NewReader(""), Out: &served, Err: &served})
	}()
	require.Eventually(t, func() bool { return strings.Contains(served.String(), "serve: leading") }, cmdtest.Settle, 10*time.Millisecond)

	out, errs, err := cmdtest.Dockhand(t, "check")
	require.NoError(t, err)
	require.Contains(t, errs, "check-1 handed to serve (pid ")
	require.Contains(t, out, "\nPassed.\n", "the check's heading named what it checks")

	out, _, err = cmdtest.Dockhand(t, "check", "-d")
	require.NoError(t, err)
	require.Contains(t, out, "check-2 queued; serve (pid ")

	var standby cmdtest.SyncBuffer
	second, stopSecond := context.WithCancel(t.Context())
	secondDone := make(chan error)
	go func() {
		secondDone <- cmdtest.RunFor(t, second, []string{"serve"}, term.Streams{In: strings.NewReader(""), Out: &standby, Err: &standby})
	}()
	require.Eventually(t, func() bool { return strings.Contains(standby.String(), "serve: standing by; another instance (pid ") }, cmdtest.Settle, 10*time.Millisecond)
	drained, _, err := cmdtest.Dockhand(t, "serve", "--drain")
	require.NoError(t, err)
	require.Contains(t, drained, "leads and runs the queue; nothing to drain here")

	stopSecond()
	require.NoError(t, <-secondDone)
	stop()
	require.NoError(t, <-done)
	require.Contains(t, served.String(), "check-1 jq-update: passed\n")
	require.Contains(t, served.String(), "serve: stopped\n")
}

// A serve whose executable is replaced, as port upgrade replaces it,
// finishes what it runs and stops, saying why, so launchd starts the new
// build; it had gone on running the old one (the rc3 full stage, A7).
func TestServeStopsForAnUpgrade(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	executable := filepath.Join(t.TempDir(), "dockhand")
	require.NoError(t, os.WriteFile(executable, []byte("rc4"), 0o755))
	cmdtest.Hosted(t).Executable = func(bool) string { return executable }
	var served cmdtest.SyncBuffer
	done := make(chan error)
	go func() {
		done <- cmdtest.RunFor(t, t.Context(), []string{"serve"}, term.Streams{In: strings.NewReader(""), Out: &served, Err: &served})
	}()
	require.Eventually(t, func() bool { return strings.Contains(served.String(), "serve: leading") }, cmdtest.Settle, 10*time.Millisecond)
	// The new build is a new file at the same path.
	next := executable + ".new"
	require.NoError(t, os.WriteFile(next, []byte("rc5 build"), 0o755))
	require.NoError(t, os.Rename(next, executable))
	select {
	case err := <-done:
		require.NoError(t, err, "launchd restarts an agent that exits")
	case <-time.After(cmdtest.Settle):
		t.Fatalf("serve didn't stop for the upgrade: %s", served.String())
	}
	require.Contains(t, served.String(), "serve: upgrade detected; waiting for active checks to finish before exiting so launchd can start the new version\nserve: stopped for the upgrade\n")
}

// A serve whose executable is gone past an upgrade's moment takes dockhand
// as uninstalled, and stops, saying how to remove the agent (the rc6 full
// stage, A8).
func TestServeStopsWhenUninstalled(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	executable := filepath.Join(t.TempDir(), "dockhand")
	require.NoError(t, os.WriteFile(executable, []byte("rc6"), 0o755))
	cmdtest.Hosted(t).Executable = func(bool) string { return executable }
	cmdtest.Hosted(t).Timings.ServeGrace = 200 * time.Millisecond
	var served cmdtest.SyncBuffer
	done := make(chan error)
	go func() {
		done <- cmdtest.RunFor(t, t.Context(), []string{"serve"}, term.Streams{In: strings.NewReader(""), Out: &served, Err: &served})
	}()
	require.Eventually(t, func() bool { return strings.Contains(served.String(), "serve: leading") }, cmdtest.Settle, 10*time.Millisecond)
	require.NoError(t, os.Remove(executable))
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(cmdtest.Settle):
		t.Fatalf("serve didn't stop once uninstalled: %s", served.String())
	}
	require.Contains(t, served.String(), "serve: dockhand was uninstalled; stopping once its checks end. serve --uninstall removes the agent\nserve: stopped, as dockhand was uninstalled\n")
}

// status says when serve runs another build than its own.
func TestStatusSaysServesOtherBuild(t *testing.T) {
	t.Parallel()
	line := view.ServeWords(engine.ServeState{Running: true, PID: 7, Build: "v0.3.0-rc3"})
	require.Contains(t, line, "serve: running (pid 7) · running dockhand v0.3.0-rc3; this command is ")
	require.Equal(t, "serve: running (pid 7) · queue: empty", view.ServeWords(engine.ServeState{Running: true, PID: 7}), "a serve that didn't say runs as this does")
}

// withLaunchd stands launchctl in for serve's agent: launchd loads the
// agent at a bootstrap and lets it go at a bootout, after bootout returns
// for prints, as launchd does (the rc10 full stage's B7). It keeps every
// call but a print.
func withLaunchd(t *testing.T, printsUntilGone int) *[][]string {
	t.Helper()
	var calls [][]string
	loaded, going := false, 0
	cmdtest.Hosted(t).Launchctl = func(_ context.Context, args ...string) ([]byte, error) {
		switch args[0] {
		case "print":
			if loaded && going > 0 {
				if going--; going == 0 {
					loaded = false
				}
			}
			if !loaded {
				return []byte("Could not find service \"" + AgentLabel + "\" in domain for port"), errors.New("exit status 113")
			}
			return []byte("state = running"), nil
		case "bootstrap":
			loaded, going = true, 0
		case "bootout":
			going = printsUntilGone + 1
		}
		calls = append(calls, args)
		return nil, nil
	}
	return &calls
}

func TestServeInstallsALaunchdAgent(t *testing.T) {
	t.Parallel()
	w := cmdtest.CheckedBranch(t)
	calls := withLaunchd(t, 2)
	cmdtest.Hosted(t).OS = "linux"
	_, _, err := cmdtest.Dockhand(t, "serve", "--install")
	require.ErrorContains(t, err, "serve --install requires macOS")

	cmdtest.Hosted(t).OS = "darwin"
	out, _, err := cmdtest.Dockhand(t, "serve", "--install")
	require.NoError(t, err)
	require.Contains(t, out, "serve now starts at login and restarts if it stops.\n  Agent  ~/Library/LaunchAgents/"+AgentLabel+".plist\n  Log    ~/.dockhand/logs/serve.log\n")
	plist, err := os.ReadFile(filepath.Join(w.Home, "Library", "LaunchAgents", AgentLabel+".plist"))
	require.NoError(t, err)
	require.Contains(t, string(plist), "<string>serve</string><string>--tree</string><string>"+w.Clone+"</string>")
	// Kept alive while dockhand is installed, and not once it's
	// uninstalled, which left launchd respawning a missing program.
	require.Regexp(t, `<key>KeepAlive</key><dict><key>PathState</key><dict><key>[^<]+</key><true/></dict></dict>`, string(plist))
	require.Contains(t, string(plist), "<key>ProcessType</key><string>Background</string>")
	require.NotContains(t, string(plist), "--no-notify")
	require.Len(t, *calls, 2)
	require.Equal(t, "bootout", (*calls)[0][0])
	require.Equal(t, "bootstrap", (*calls)[1][0])
	// Flags given beside --install shape the agent's runs.
	_, _, err = cmdtest.Dockhand(t, "serve", "--install", "--no-notify", "--ship-to", "submit")
	require.NoError(t, err)
	plist, err = os.ReadFile(filepath.Join(w.Home, "Library", "LaunchAgents", AgentLabel+".plist"))
	require.NoError(t, err)
	require.Contains(t, string(plist), "<string>"+filepath.Join(w.Home, ".dockhand", "dockhand.db")+"</string><string>--ship-to=submit</string><string>--no-notify</string></array>")
	*calls = nil

	// Uninstall waits until launchd has let the agent go, which it does
	// after bootout returns, so a status read at once doesn't find it.
	out, _, err = cmdtest.Dockhand(t, "serve", "--uninstall")
	require.NoError(t, err)
	require.Contains(t, out, "Removed the serve agent")
	require.Len(t, *calls, 1, "uninstall only boots it out")
	require.NoFileExists(t, filepath.Join(w.Home, "Library", "LaunchAgents", AgentLabel+".plist"))
	loaded, err := macos.LaunchAgent{Label: AgentLabel, Launchctl: cmdtest.Hosted(t).Launchctl}.Loaded(t.Context())
	require.NoError(t, err)
	require.False(t, loaded, "gone when uninstall returns")
}

func TestServeCleansUpAfterAMergeOnceADay(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	g := cmdtest.WithGitHub(t, w)
	cmdtest.Hosted(t).Timings.ServePoll = 20 * time.Millisecond
	cmdtest.Setenv(t, "DOCKHAND_INDEX_CACHE", t.TempDir())
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	cmdtest.Setenv(t, "MACPORTS_TREE", dir)
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	g.PRs[34901].State = forge.PullRequestMerged
	cmdtest.Setenv(t, "MACPORTS_TREE", w.Clone)
	_, _, err = cmdtest.Dockhand(t, "status", "--refresh")
	require.NoError(t, err)

	serveFor := func(until string) string {
		ctx, stop := context.WithCancel(t.Context())
		var served cmdtest.SyncBuffer
		done := make(chan error)
		go func() {
			done <- cmdtest.RunFor(t, ctx, []string{"serve"}, term.Streams{In: strings.NewReader(""), Out: &served, Err: &served})
		}()
		require.Eventually(t, func() bool { return strings.Contains(served.String(), until) }, cmdtest.Settle, 10*time.Millisecond)
		time.Sleep(100 * time.Millisecond)
		stop()
		require.NoError(t, <-done)
		return served.String()
	}

	require.NoError(t, os.MkdirAll(filepath.Join(w.Home, ".dockhand"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.Home, ".dockhand", "config.toml"), []byte("[cleanup]\nautomatic = false\n"), 0o644))
	out := serveFor("serve: leading")
	require.NotContains(t, out, "cleaned up", "turned off")
	require.DirExists(t, dir)

	require.NoError(t, os.Remove(filepath.Join(w.Home, ".dockhand", "config.toml")))
	out = serveFor("jq-update: cleaned up after the merge: removed worktree ")
	require.Contains(t, out, ", branch dockhand/jq-update, ada/macports-ports:dockhand/jq-update\n")
	require.NoDirExists(t, dir)
	require.FileExists(t, cmdtest.CleanupStamp(t, w.Home))

	out = serveFor("serve: leading")
	require.NotContains(t, out, "cleaned up", "once a day")
}

func TestServeRunsChecksUpToEachProvidersCapacity(t *testing.T) {
	t.Parallel()
	w := cmdtest.CheckedBranch(t)
	// The script holds each check until the go file appears, saying which
	// ones started.
	script := filepath.Join(w.Home, "bin", "build-ports")
	require.NoError(t, os.WriteFile(script, []byte(`#!/bin/sh
dir=$(dirname "$1")
run=$(basename "$(dirname "$dir")")
: > "$HOME/started-$run"
while [ ! -f "$HOME/go" ]; do sleep 0.02; done
cat > "$dir/result.json" <<JSON
{"version": 1, "targets": [{"id": "jq", "outcome": "passed"}]}
JSON
`), 0o755))
	configure := func(capacity int) {
		require.NoError(t, os.WriteFile(filepath.Join(w.Home, ".dockhand", "config.toml"),
			[]byte(fmt.Sprintf("[providers.command]\nrun = \"~/bin/build-ports\"\ncapacity = %d\n", capacity)), 0o644))
	}
	_, _, err := cmdtest.Dockhand(t, "check", "-d")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "start", "jq-other")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-other"))
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = cmdtest.Dockhand(t, "check", "-d")
	require.NoError(t, err)

	started := func(run string) bool {
		_, err := os.Stat(filepath.Join(w.Home, "started-"+run))
		return err == nil
	}
	serveUntil := func(capacity int, check func()) string {
		configure(capacity)
		var served cmdtest.SyncBuffer
		done := make(chan error)
		go func() {
			done <- cmdtest.RunFor(t, t.Context(), []string{"serve", "--drain"}, term.Streams{In: strings.NewReader(""), Out: &served, Err: &served})
		}()
		check()
		require.NoError(t, os.WriteFile(filepath.Join(w.Home, "go"), nil, 0o644))
		require.NoError(t, <-done)
		return served.String()
	}

	out := serveUntil(2, func() {
		require.Eventually(t, func() bool { return started("check-1") && started("check-2") }, cmdtest.Settle, 10*time.Millisecond,
			"with capacity 2, both checks run at once")
	})
	require.Contains(t, out, "builds on command (2 checks at a time)")
	require.Contains(t, out, "check-1 jq-update: passed\n")
	require.Contains(t, out, "check-2 jq-other: passed\n")

	// With capacity 1, the second waits for the first.
	for _, name := range []string{"go", "started-check-1", "started-check-2"} {
		require.NoError(t, os.Remove(filepath.Join(w.Home, name)))
	}
	_, _, err = cmdtest.Dockhand(t, "check", "-d")
	require.NoError(t, err)
	cmdtest.Setenv(t, "MACPORTS_TREE", filepath.Join(w.Home, "Source", "macports-branches", "jq-update"))
	_, _, err = cmdtest.Dockhand(t, "check", "-d")
	require.NoError(t, err)
	serveUntil(1, func() {
		require.Eventually(t, func() bool { return started("check-3") || started("check-4") }, cmdtest.Settle, 10*time.Millisecond)
		time.Sleep(200 * time.Millisecond)
		require.False(t, started("check-3") && started("check-4"), "with capacity 1, one check at a time")
	})
}

// serve.notify turns notifications on, the default, or off; --no-notify
// turns them off for one run.
func TestNotificationsCanBeTurnedOff(t *testing.T) {
	t.Parallel()
	e := &engine.Engine{Providers: map[string]buildenv.Provider{}}
	require.NotNil(t, serveOptions(cmdtest.Hosted(t), e, config.File{}, io.Discard, false, "list", true).Notify)
	require.Nil(t, serveOptions(cmdtest.Hosted(t), e, config.File{}, io.Discard, false, "list", false).Notify)
	off := false
	file := config.File{Serve: config.Serve{Notify: &off}}
	require.False(t, file.Serve.Notifies())
}

// serve's agent runs with the settings it was installed under: --git, the
// database, and the environment variables dockhand reads, each path made
// absolute, since launchd starts it in /. A token is never written, since
// anyone on the Mac can read the agent's file; the install says so.
func TestServeInstallCarriesTheSettingsItRanWith(t *testing.T) {
	t.Parallel()
	w := cmdtest.CheckedBranch(t)
	withLaunchd(t, 0)
	cmdtest.Hosted(t).OS = "darwin"
	git, err := exec.LookPath("git")
	require.NoError(t, err)
	cmdtest.Setenv(t, "GIT_BIN", git)
	require.NoError(t, os.MkdirAll(filepath.Join(w.Home, "tart"), 0o755))
	cmdtest.Chdir(t, w.Home)
	cmdtest.Setenv(t, "TART_HOME", "tart")
	cmdtest.Setenv(t, "GH_TOKEN", "ghp_secret")

	out, _, err := cmdtest.Dockhand(t, "serve", "--install", "--db", ".dockhand/dockhand.db")
	require.NoError(t, err)
	require.Contains(t, out, "  With   DOCKHAND_UPSTREAM, TART_HOME, as set now\n")
	require.Contains(t, out, "GH_TOKEN is omitted from the agent file because that file is readable by other users on this Mac")
	plist, err := os.ReadFile(filepath.Join(w.Home, "Library", "LaunchAgents", AgentLabel+".plist"))
	require.NoError(t, err)
	agent := string(plist)
	require.Contains(t, agent, "<string>--db</string><string>"+filepath.Join(w.Home, ".dockhand", "dockhand.db")+"</string><string>--git</string><string>"+git+"</string>")
	require.Contains(t, agent, "<key>DOCKHAND_UPSTREAM</key><string>"+w.Upstream+"</string>")
	require.Contains(t, agent, "<key>TART_HOME</key><string>"+filepath.Join(w.Home, "tart")+"</string>", "made absolute")
	require.NotContains(t, agent, "ghp_secret")
	require.NotContains(t, agent, "GH_TOKEN")
}

// serve acts on GitHub only as the account it started as: with no login
// it says so once, as with another account's, and says when its own comes
// back; status says why it isn't acting (the rc6 full stage, D-C2).
func TestServeActsOnlyAsTheAccountItStartedAs(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	var mu sync.Mutex
	answers := []string{"ada", "", "", "bob", "revoked", "revoked", "ada"}
	cmdtest.Hosted(t).Timings.ServeIdentity = 50 * time.Millisecond
	cmdtest.Hosted(t).Identity = func(*github.Runtime) func(context.Context) (string, error) {
		return func(context.Context) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			answer := answers[0]
			if len(answers) > 1 {
				answers = answers[1:]
			}
			switch answer {
			case "":
				return "", github.ErrNoCredentials
			case "revoked":
				return "", fmt.Errorf("%w: GitHub rejected the credential from Dockhand macOS Keychain", github.ErrAuthentication)
			}
			return answer, nil
		}
	}
	ctx, stop := context.WithCancel(t.Context())
	var served cmdtest.SyncBuffer
	done := make(chan error)
	go func() {
		done <- cmdtest.RunFor(t, ctx, []string{"serve"}, term.Streams{In: strings.NewReader(""), Out: &served, Err: &served})
	}()
	require.Eventually(t, func() bool { return strings.Contains(served.String(), "logged in again as ada") }, cmdtest.Settle, 10*time.Millisecond)
	stop()
	require.NoError(t, <-done)
	out := served.String()
	require.Equal(t, 1, strings.Count(out, "serve: no GitHub credentials are available. Pull requests will be read anonymously; submissions, pushes, and GitHub checks are paused. Run dockhand setup github to sign in\n"), out)
	require.Contains(t, out, "serve: GitHub is now signed in as bob; serve started as ada. GitHub writes are paused. Sign in as ada again, or restart serve to use the current account\n")
	require.Equal(t, 1, strings.Count(out, "serve: GitHub rejected the credentials. GitHub writes are paused; run dockhand setup github to sign in again\n"), out)
	require.Contains(t, out, "serve: logged in again as ada; acting on GitHub again\n")

	require.Equal(t, "serve: running (pid 7) · not acting on GitHub: no GitHub login · queue: empty", view.ServeWords(engine.ServeState{Running: true, PID: 7, NotActing: "no GitHub login"}))
}

// A login revoked, then one serve can't read for a while, as a Keychain
// that won't answer the agent, then a new one: serve says once why it
// couldn't read it, where it kept its state in silence, and that it's
// logged in again once it can (the rc8 full stage's D-C3).
func TestServeSaysALoginItCantRead(t *testing.T) {
	t.Parallel()
	cmdtest.CheckedBranch(t)
	var mu sync.Mutex
	answers := []string{"ada", "revoked", "unread", "unread", "ada"}
	cmdtest.Hosted(t).Timings.ServeIdentity = 50 * time.Millisecond
	cmdtest.Hosted(t).Identity = func(*github.Runtime) func(context.Context) (string, error) {
		return func(context.Context) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			answer := answers[0]
			if len(answers) > 1 {
				answers = answers[1:]
			}
			switch answer {
			case "revoked":
				return "", fmt.Errorf("%w: GitHub rejected the credential from Dockhand macOS Keychain", github.ErrAuthentication)
			case "unread":
				return "", errors.New("github: reading saved credential: the keychain won't answer")
			}
			return answer, nil
		}
	}
	ctx, stop := context.WithCancel(t.Context())
	var served cmdtest.SyncBuffer
	done := make(chan error)
	go func() {
		done <- cmdtest.RunFor(t, ctx, []string{"serve"}, term.Streams{In: strings.NewReader(""), Out: &served, Err: &served})
	}()
	require.Eventually(t, func() bool { return strings.Contains(served.String(), "logged in again as ada") }, cmdtest.Settle, 10*time.Millisecond)
	stop()
	require.NoError(t, <-done)
	out := served.String()
	require.Equal(t, 1, strings.Count(out, "serve: could not verify the GitHub account (github: reading saved credential: the keychain won't answer). Continuing with the current authentication state; retrying each minute\n"), out)
	require.Contains(t, out, "serve: logged in again as ada; acting on GitHub again\n")

	require.Equal(t, "serve: running (pid 7) · not acting on GitHub: GitHub rejected the login; dockhand setup github · couldn't read its GitHub login: the keychain won't answer · queue: empty",
		view.ServeWords(engine.ServeState{Running: true, PID: 7, NotActing: "GitHub rejected the login; dockhand setup github", LoginProblem: "the keychain won't answer"}))
}
