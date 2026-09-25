package command

import (
	"os"
	"testing"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// TestMain keeps the command tests out of the person's own dockhand: a
// fresh HOME, so ~/.dockhand/dockhand.db is the tests', and none of the
// variables that point dockhand at a real tree, database, configuration,
// upstream, or token. On a Mac with MACPORTS_TREE set, a bare dockhand
// otherwise registered the person's checkout in their real database.
func TestMain(m *testing.M) {
	for _, name := range []string{"MACPORTS_TREE", "DOCKHAND_DB", "DOCKHAND_CONFIG", "DOCKHAND_UPSTREAM", "DOCKHAND_GITHUB_CLIENT_ID", "GH_TOKEN", "GITHUB_TOKEN"} {
		os.Unsetenv(name)
	}
	cleanup := testsupport.IsolateHome()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
