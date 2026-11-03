package macos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/subprocess"
)

// LaunchAgent is a per-user launchd agent: its label, and where its
// property list is kept, in ~/Library/LaunchAgents.
type LaunchAgent struct {
	Label string
	Path  string
	// Launchctl runs launchctl with its arguments, and returns what it
	// wrote; /bin/launchctl where nil.
	Launchctl func(ctx context.Context, args ...string) ([]byte, error)
	// Wait is how long Install and Remove wait on launchd, and Poll how
	// often they ask it; thirty seconds and a quarter of a second where
	// zero.
	Wait, Poll time.Duration
}

// errStillLoaded is an agent launchd still has loaded when the wait for it
// to let go ran out.
var errStillLoaded = errors.New("launchd still has the agent loaded")

// agentWait is how long an install or a removal waits on launchd by
// default.
const agentWait = 30 * time.Second

func (a LaunchAgent) domain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }
func (a LaunchAgent) target() string { return a.domain() + "/" + a.Label }

func (a LaunchAgent) launchctl(ctx context.Context, args ...string) ([]byte, error) {
	if a.Launchctl != nil {
		return a.Launchctl(ctx, args...)
	}
	result, err := subprocess.Run(ctx, subprocess.Spec{Tool: "launchctl", Path: "/bin/launchctl", Command: strings.Join(args, " "), Args: args, Combined: true, Limit: 1 << 20})
	return result.Output, err
}

// Loaded reports whether launchd has the agent loaded, as launchctl
// print says: a service it can't find isn't.
func (a LaunchAgent) Loaded(ctx context.Context) (bool, error) {
	output, err := a.launchctl(ctx, "print", a.target())
	switch {
	case err == nil:
		return true, nil
	case strings.Contains(string(output), "Could not find service"):
		return false, nil
	}
	return false, fmt.Errorf("launchctl print %s: %w", a.target(), err)
}

// await asks launchd until the agent is loaded or not, as wanted, up to
// the wait; errStillLoaded, or its opposite, where it ran out.
func (a LaunchAgent) await(ctx context.Context, loaded bool) error {
	wait, poll := a.Wait, a.Poll
	if wait <= 0 {
		wait = agentWait
	}
	if poll <= 0 {
		poll = 250 * time.Millisecond
	}
	deadline := time.Now().Add(wait)
	for {
		now, err := a.Loaded(ctx)
		if err != nil || now == loaded {
			return err
		}
		if time.Now().After(deadline) {
			if loaded {
				return fmt.Errorf("launchd didn't load the agent within %s; launchctl print %s says why", wait, a.target())
			}
			return fmt.Errorf("%w %s after it was booted out; launchctl print %s shows it", errStillLoaded, wait, a.target())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

// Remove boots the agent out, waits until launchd has let it go, and
// removes its property list. launchctl bootout returns before launchd has
// unloaded the agent, so a status read at once found serve's still there
// (the rc10 full stage's B7, and rc6's). One not loaded is removed as it
// is; one launchd still holds when the wait runs out is errStillLoaded's,
// its property list removed all the same.
func (a LaunchAgent) Remove(ctx context.Context) error {
	// Booting out an agent that isn't loaded is no error here.
	_, _ = a.launchctl(ctx, "bootout", a.target())
	waited := a.await(ctx, false)
	if err := os.Remove(a.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return waited
}

// Install writes the job's property list and bootstraps it, once launchd
// has let go of the agent it replaces, which a bootstrap after a bootout
// met still loaded; then waits until launchd has it loaded.
func (a LaunchAgent) Install(ctx context.Context, job LaunchdJob) error {
	_, _ = a.launchctl(ctx, "bootout", a.target())
	if err := a.await(ctx, false); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.Path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(a.Path, job.Plist(), 0o644); err != nil {
		return err
	}
	if output, err := a.launchctl(ctx, "bootstrap", a.domain(), a.Path); err != nil {
		return fmt.Errorf("launchctl bootstrap: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return a.await(ctx, true)
}

// Write writes job's property list in the agent's place, which launchd
// reads when it next loads the agent (Reload, ReloadCommand).
func (a LaunchAgent) Write(job LaunchdJob) error {
	return os.WriteFile(a.Path, job.Plist(), 0o644)
}

// Reload has launchd load the agent's property list again, bootout and
// bootstrap, through Launchctl: a test's, which answers at once.
func (a LaunchAgent) Reload(ctx context.Context) error {
	if _, err := a.launchctl(ctx, "bootout", a.target()); err != nil {
		return err
	}
	_, err := a.launchctl(ctx, "bootstrap", a.domain(), a.Path)
	return err
}

// ReloadCommand is Reload as a program to start in a session of its own,
// as the agent itself must: launchd's bootout would end the agent before
// it could bootstrap the new one, so the program waits a second, for the
// agent to exit, and asks.
func (a LaunchAgent) ReloadCommand() []string {
	return []string{"/bin/sh", "-c", fmt.Sprintf("sleep 1; /bin/launchctl bootout %s; /bin/launchctl bootstrap %s %s", a.target(), a.domain(), shellQuote(a.Path))}
}

// shellQuote is a path quoted for sh.
func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}
