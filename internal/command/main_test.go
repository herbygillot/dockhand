package command

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// signedIn stands in for the GitHub CLI: signed in as login, it records
// what it marks ready; with no login, it isn't installed.
type signedIn struct {
	login   string
	readied *[]int
}

func (c signedIn) Login(context.Context) (string, error) {
	if c.login == "" {
		return "", errors.New("the GitHub CLI isn't installed")
	}
	return c.login, nil
}

func (c signedIn) MarkReady(_ context.Context, ref forge.PullRequestRef) error {
	*c.readied = append(*c.readied, ref.Number)
	return nil
}

// TestMain keeps the command tests out of the person's own dockhand: a
// fresh HOME, so ~/.dockhand/dockhand.db is the tests', and none of the
// variables that point dockhand at a real tree, database, configuration,
// upstream, or token. On a Mac with MACPORTS_TREE set, a bare dockhand
// otherwise registered the person's checkout in their real database.
func TestMain(m *testing.M) {
	for _, name := range []string{"MACPORTS_TREE", "DOCKHAND_DB", "DOCKHAND_CONFIG", "DOCKHAND_UPSTREAM", "DOCKHAND_PULL_REQUESTS", "DOCKHAND_GITHUB_CLIENT_ID", "GH_TOKEN", "GITHUB_TOKEN"} {
		os.Unsetenv(name)
	}
	lookTart = func(string) (string, error) { return "", exec.ErrNotFound }
	// Nor the GitHub CLI this Mac has, signed in as its person.
	gitHubCLI = signedIn{}
	// The executable is the test binary, which isn't dockhand.
	startCleanup = func(engine.Options) error { return nil }
	cleanup := testsupport.IsolateHome()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
