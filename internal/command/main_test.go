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
	// Nor a shell's Tart homes, caches, or proxy, as the acceptance
	// stage's (the M1's run at 10aac0c3: serve's test read its
	// DOCKHAND_UPSTREAM's neighbours into the agent).
	for _, name := range []string{"MACPORTS_TREE", "DOCKHAND_DB", "DOCKHAND_CONFIG", "DOCKHAND_UPSTREAM", "DOCKHAND_PULL_REQUESTS", "DOCKHAND_GITHUB_CLIENT_ID", "GH_TOKEN", "GITHUB_TOKEN",
		"DOCKHAND_INDEX_MIRROR", "DOCKHAND_INDEX_CACHE", "DOCKHAND_READING_CACHE", "DOCKHAND_TART_HOME", "TART_HOME", "DOCKHAND_SSH_DIR", "GIT_SSH_COMMAND", "HTTPS_PROXY", "HTTP_PROXY"} {
		os.Unsetenv(name)
	}
	lookTart = func(string) (string, error) { return "", exec.ErrNotFound }
	// Every test's Mac takes Tart, whichever Mac runs it, but where it
	// says it's an Intel Mac's (onIntel).
	tartSupported = func() bool { return true }
	// Nor the GitHub CLI this Mac has, signed in as its person.
	gitHubCLI = signedIn{}
	// Nor this Mac's GitHub login, read afresh by serve's identity check:
	// a serve test's login is no one's, unless the test gives it one.
	serveIdentity = func() func(context.Context) (string, error) { return nil }
	// The executable is the test binary, which isn't dockhand.
	startCleanup = func(engine.Options) error { return nil }
	cleanup := testsupport.IsolateHome()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
