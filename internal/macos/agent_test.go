package macos

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// launchd stands in for launchctl: it loads an agent at a bootstrap, and
// lets it go a number of prints after a bootout returns, as launchd does;
// forever where that's negative.
type launchd struct {
	loaded bool
	going  int
	calls  []string
}

func (l *launchd) run(_ context.Context, args ...string) ([]byte, error) {
	l.calls = append(l.calls, args[0])
	switch args[0] {
	case "print":
		if l.loaded && l.going > 0 {
			if l.going--; l.going == 0 {
				l.loaded = false
			}
		}
		if !l.loaded {
			return []byte(`Could not find service "demo" in domain for port`), errors.New("exit status 113")
		}
		return []byte("state = running"), nil
	case "bootstrap":
		l.loaded = true
	}
	return nil, nil
}

// An agent's removal waits until launchd has let it go, which it does
// after bootout returns, and says it where the wait runs out; an install
// waits for the agent it replaces to go before bootstrapping, and for the
// new one to load (the rc10 full stage's B7).
func TestALaunchAgentWaitsForLaunchd(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "LaunchAgents", "demo.plist")
	l := &launchd{}
	agent := LaunchAgent{Label: "demo", Path: path, Launchctl: l.run, Poll: time.Millisecond, Wait: time.Second}
	require.NoError(t, agent.Install(t.Context(), LaunchdJob{Label: "demo", Arguments: []string{"/bin/true"}}))
	require.FileExists(t, path)
	require.True(t, l.loaded)

	l.going = 3
	l.calls = nil
	require.NoError(t, agent.Install(t.Context(), LaunchdJob{Label: "demo", Arguments: []string{"/bin/true"}}))
	require.Equal(t, []string{"bootout", "print", "print", "print", "bootstrap", "print"}, l.calls, "the old agent let go before the new one's bootstrap")

	l.going = 2
	require.NoError(t, agent.Remove(t.Context()))
	require.False(t, l.loaded, "gone when Remove returns")
	require.NoFileExists(t, path)
	require.NoError(t, agent.Remove(t.Context()), "one not there is removed as it is")

	require.NoError(t, agent.Install(t.Context(), LaunchdJob{Label: "demo", Arguments: []string{"/bin/true"}}))
	l.going = -1
	agent.Wait = 20 * time.Millisecond
	err := agent.Remove(t.Context())
	require.ErrorIs(t, err, errStillLoaded)
	require.NoFileExists(t, path, "its property list goes all the same")
	_, statErr := os.Stat(path)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}
