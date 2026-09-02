package gh

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tool"
)

// These pin the gh runner's words end to end through a Finder: the
// miss with its remedy and the "gh <subcommand>: <stderr>" failure,
// the same bytes the exit table types by hand.

// ghScript is a finder whose gh is a shell script with the given body,
// every other lookup answered for real.
func ghScript(t *testing.T, body string) *tool.Finder {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gh")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755))
	return tool.NewFinder(func(name string) (string, error) {
		if name == string(tool.Gh) {
			return path, nil
		}
		return exec.LookPath(name)
	})
}

func TestRealGhOutNamesTheRemedyWhenGhIsMissing(t *testing.T) {
	absent := tool.NewFinder(func(string) (string, error) { return "", errors.New("absent") })
	_, err := RealGhOut(absent)(context.Background(), "api", "user")
	require.ErrorIs(t, err, tool.ErrNotFound)
	assert.Equal(t, "gh not found on PATH (`port install gh`)", err.Error())
}

func TestRealGhOutWordsAFailureWithTheSubcommandAndStderr(t *testing.T) {
	gh := RealGhOut(ghScript(t, "echo ' HTTP 401 ' >&2\nexit 1\n"))
	_, err := gh(context.Background(), "api", "user")
	require.Error(t, err)
	assert.Equal(t, "gh api: HTTP 401", err.Error())

	// With nothing on stderr, os/exec's own words stand in.
	gh = RealGhOut(ghScript(t, "exit 2\n"))
	_, err = gh(context.Background(), "pr", "create")
	require.Error(t, err)
	assert.Equal(t, "gh pr: exit status 2", err.Error())
}

func TestRealGhOutReturnsStdout(t *testing.T) {
	gh := RealGhOut(ghScript(t, "echo \"$@\"\necho noise >&2\n"))
	out, err := gh(context.Background(), "api", "user", "-q", ".login")
	require.NoError(t, err)
	assert.Equal(t, "api user -q .login\n", out, "stdout alone is the answer")
}

func TestOwnerRepoFromURLReadsBothSpellings(t *testing.T) {
	for _, tc := range []struct{ url, owner, repo string }{
		{"git@github.com:macports/macports-ports.git", "macports", "macports-ports"},
		{"https://github.com/macports/macports-ports", "macports", "macports-ports"},
		{"https://github.com/macports/macports-ports.git", "macports", "macports-ports"},
		{"ssh://git@github.com/macports/macports-ports.git", "macports", "macports-ports"},
		// A local bare fork is a remote too: the last two path
		// segments are what a fixture's directory layout spells.
		{"/tmp/checkout/herbygillot/ports", "herbygillot", "ports"},
	} {
		o, r, ok := OwnerRepoFromURL(tc.url)
		require.True(t, ok, tc.url)
		assert.Equal(t, tc.owner, o, tc.url)
		assert.Equal(t, tc.repo, r, tc.url)
	}
	_, _, ok := OwnerRepoFromURL("nonsense")
	assert.False(t, ok, "one segment names no repository")
}
