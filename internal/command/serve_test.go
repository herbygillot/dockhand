package command

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/record"
)

// syncBuffer is a buffer two goroutines may share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func checkedBranch(t *testing.T) world {
	t.Helper()
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	poll := servePoll
	t.Cleanup(func() { servePoll = poll })
	servePoll = 20 * time.Millisecond
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "src", "macports-branches", "jq-update"))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	return w
}

func TestServeDrainsTheQueue(t *testing.T) {
	checkedBranch(t)
	_, _, err := dockhand(t, "check", "-d")
	require.NoError(t, err)
	out, _, err := dockhand(t, "serve", "--drain")
	require.NoError(t, err)
	require.Contains(t, out, "serve: leading (pid ")
	require.Contains(t, out, "builds on command · opens no pull requests; it only checks\n")
	require.Contains(t, out, "check-1 jq-update: running\ncheck-1 jq-update: passed\nserve: the queue is empty\n")
	out, _, err = dockhand(t, "queue")
	require.NoError(t, err)
	require.Equal(t, "serve: not running · queue: empty\n", out)
}

func TestACheckIsHandedToServe(t *testing.T) {
	checkedBranch(t)
	ctx, stop := context.WithCancel(t.Context())
	var served syncBuffer
	done := make(chan error)
	go func() {
		done <- Run(ctx, []string{"serve"}, Streams{In: strings.NewReader(""), Out: &served, Err: &served})
	}()
	require.Eventually(t, func() bool { return strings.Contains(served.String(), "serve: leading") }, 5*time.Second, 10*time.Millisecond)

	out, errs, err := dockhand(t, "check")
	require.NoError(t, err)
	require.Contains(t, errs, "check-1 handed to serve (pid ")
	require.Contains(t, out, "Passed for snapshot 1.")

	out, _, err = dockhand(t, "check", "-d")
	require.NoError(t, err)
	require.Contains(t, out, "check-2 queued; serve (pid ")

	var standby syncBuffer
	second, stopSecond := context.WithCancel(t.Context())
	secondDone := make(chan error)
	go func() {
		secondDone <- Run(second, []string{"serve"}, Streams{In: strings.NewReader(""), Out: &standby, Err: &standby})
	}()
	require.Eventually(t, func() bool { return strings.Contains(standby.String(), "serve: standing by; serve (pid ") }, 5*time.Second, 10*time.Millisecond)
	drained, _, err := dockhand(t, "serve", "--drain")
	require.NoError(t, err)
	require.Contains(t, drained, "leads and runs the queue; nothing to drain here")

	stopSecond()
	require.NoError(t, <-secondDone)
	stop()
	require.NoError(t, <-done)
	require.Contains(t, served.String(), "check-1 jq-update: passed\n")
	require.Contains(t, served.String(), "serve: stopped\n")
}

func TestServeInstallsALaunchdAgent(t *testing.T) {
	w := checkedBranch(t)
	var calls [][]string
	realLaunchctl, realOS := launchctl, agentOS
	t.Cleanup(func() { launchctl, agentOS = realLaunchctl, realOS })
	launchctl = func(_ context.Context, args ...string) error {
		calls = append(calls, args)
		return nil
	}
	agentOS = "linux"
	_, _, err := dockhand(t, "serve", "--install")
	require.ErrorContains(t, err, "launchd agent, which is macOS's")

	agentOS = "darwin"
	out, _, err := dockhand(t, "serve", "--install")
	require.NoError(t, err)
	require.Contains(t, out, "serve now starts at login and restarts if it stops.\n  Agent  ~/Library/LaunchAgents/"+AgentLabel+".plist\n  Log    ~/.dockhand/logs/serve.log\n")
	plist, err := os.ReadFile(filepath.Join(w.home, "Library", "LaunchAgents", AgentLabel+".plist"))
	require.NoError(t, err)
	require.Contains(t, string(plist), "<string>serve</string>\n    <string>--tree</string>\n    <string>"+w.clone+"</string>\n")
	require.Contains(t, string(plist), "<key>KeepAlive</key>\n  <true/>")
	require.Len(t, calls, 2)
	require.Equal(t, "bootout", calls[0][0])
	require.Equal(t, "bootstrap", calls[1][0])

	out, _, err = dockhand(t, "serve", "--uninstall")
	require.NoError(t, err)
	require.Contains(t, out, "Removed the serve agent")
	require.NoFileExists(t, filepath.Join(w.home, "Library", "LaunchAgents", AgentLabel+".plist"))
}

func TestServeCleansUpAfterAMergeOnceADay(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	g := withGitHub(t, w)
	poll := servePoll
	t.Cleanup(func() { servePoll = poll })
	servePoll = 20 * time.Millisecond
	t.Setenv("DOCKHAND_INDEX_CACHE", t.TempDir())
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "src", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	_, _, err = dockhand(t, "submit", "--no-check", "--yes")
	require.NoError(t, err)
	g.prs[0].State = record.PullRequestMerged
	t.Setenv("MACPORTS_TREE", w.clone)
	_, _, err = dockhand(t, "status", "--refresh")
	require.NoError(t, err)

	serveFor := func(until string) string {
		ctx, stop := context.WithCancel(t.Context())
		var served syncBuffer
		done := make(chan error)
		go func() {
			done <- Run(ctx, []string{"serve"}, Streams{In: strings.NewReader(""), Out: &served, Err: &served})
		}()
		require.Eventually(t, func() bool { return strings.Contains(served.String(), until) }, 5*time.Second, 10*time.Millisecond)
		time.Sleep(100 * time.Millisecond)
		stop()
		require.NoError(t, <-done)
		return served.String()
	}

	require.NoError(t, os.MkdirAll(filepath.Join(w.home, ".dockhand"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(w.home, ".dockhand", "config.toml"), []byte("[cleanup]\nautomatic = false\n"), 0o644))
	out := serveFor("serve: leading")
	require.NotContains(t, out, "cleaned up", "turned off")
	require.DirExists(t, dir)

	require.NoError(t, os.Remove(filepath.Join(w.home, ".dockhand", "config.toml")))
	out = serveFor("jq-update: cleaned up after the merge: removed worktree ")
	require.Contains(t, out, ", branch dockhand/jq-update, ada/macports-ports:dockhand/jq-update\n")
	require.NoDirExists(t, dir)
	require.FileExists(t, filepath.Join(w.home, ".dockhand", "cleanup.stamp"))

	out = serveFor("serve: leading")
	require.NotContains(t, out, "cleaned up", "once a day")
}
