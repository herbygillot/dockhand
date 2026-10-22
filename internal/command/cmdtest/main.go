package cmdtest

import (
	"context"
	"os"
	"testing"

	"github.com/herbygillot/dockhand/internal/command/host"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/compose"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// Runner runs a command line, as the command line's root does
// (command.RunWith), which a test binary hands Main: cmdtest is below the
// root and never imports it, so a verb's external tests and the root's
// own run the same command line.
type Runner func(context.Context, settings.Invocation, term.Streams, compose.Dependencies, *host.Host) error

// runner is the test binary's, which Main set.
var runner Runner

// Main keeps a test binary's command tests out of the person's own
// dockhand: a fresh HOME, so ~/.dockhand/dockhand.db is the tests', and
// none of the variables that point dockhand at a real tree, database,
// configuration, upstream, or token. On a Mac with MACPORTS_TREE set, a
// bare dockhand otherwise registered the person's checkout in their real
// database. Nor a shell's Tart homes, caches, or proxy, as the acceptance
// stage's (the M1's run at 10aac0c3: serve's test read its
// DOCKHAND_UPSTREAM's neighbours into the agent). It runs the tests with
// run as their command line.
func Main(m *testing.M, run Runner) {
	runner = run
	for _, name := range []string{"MACPORTS_TREE", "DOCKHAND_DB", "DOCKHAND_CONFIG", "DOCKHAND_UPSTREAM", "DOCKHAND_PULL_REQUESTS", "DOCKHAND_GITHUB_CLIENT_ID", "GH_TOKEN", "GITHUB_TOKEN",
		"DOCKHAND_INDEX_MIRROR", "DOCKHAND_INDEX_CACHE", "DOCKHAND_READING_CACHE", "DOCKHAND_TART_HOME", "TART_HOME", "DOCKHAND_SSH_DIR", "GIT_SSH_COMMAND", "HTTPS_PROXY", "HTTP_PROXY"} {
		os.Unsetenv(name)
	}
	cleanup := testsupport.IsolateHome()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
