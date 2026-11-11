package command_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A command that prints only a document prints the same bytes at any
// width: a usage page, fenced blocks and all, and a completion script
// (#94).
func TestADocumentIsTheSameAtAnyWidth(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"usage", "configuration"}, {"completion", "fish"}} {
		t.Run(args[0], func(t *testing.T) {
			t.Parallel()
			cmdtest.NewWorld(t)
			cmdtest.SameAtAnyWidth(t, args...)
		})
	}
}

// A plan's diff is whole at any width, a Portfile's 130-column line
// among it, where its sentences wrap: update --plan wrote its diff
// through the wrapper (#93, #94).
func TestAPlansDiffIsWholeAtAnyWidth(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	long := "# " + strings.Repeat("a comment that runs on past any terminal ", 3)
	require := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	require(os.WriteFile(filepath.Join(w.Upstream, "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\n"+long+"\n"), 0o644))
	testsupport.Git(t, w.Upstream, "commit", "-q", "-am", "jq: 1.7.1")
	cmdtest.WithBumper(t)
	cmdtest.PayloadAtAnyWidth(t, "\n "+long+"\n", "update", "jq", "--new", "--plan")
}
