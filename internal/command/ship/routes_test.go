package ship_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// tidy's review diff, opened by "d" from ship's committing stage on a
// narrow terminal, is the payload whole: a stage's streams held the
// wrapper's switch, and the diff's long line wrapped (#94, the first of
// the three routes the issue names).
func TestTidysDiffFromShipsStageIsWhole(t *testing.T) {
	t.Parallel()
	w := cmdtest.NewWorld(t)
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithGitHub(t, w)
	_, _, err := cmdtest.Dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	tree := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
	cmdtest.Setenv(t, "MACPORTS_TREE", tree)
	_, _, err = cmdtest.Dockhand(t, "update", "jq")
	require.NoError(t, err)
	long := "long_description    jq is a lightweight and flexible command-line JSON processor, akin to sed for JSON data"
	portfile := filepath.Join(tree, "textproc/jq/Portfile")
	data, err := os.ReadFile(portfile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(portfile, append(data, []byte(long+"\n")...), 0o644))
	testsupport.Git(t, tree, "commit", "-q", "-am", "wip")

	var out, errs bytes.Buffer
	streams := term.Streams{In: strings.NewReader("y\nd\nq\n"), Out: &out, Err: &errs}.AsTerminal().WithDisplay(term.Display{Columns: 40})
	_ = cmdtest.RunFor(t, t.Context(), []string{"ship", "jq", "--to", "commit"}, streams)
	require.Contains(t, errs.String(), "Review diff [d]", "the committing stage asked: %s\n%s", out.String(), errs.String())
	require.Contains(t, out.String(), "+"+long+"\n", "the diff's line, whole")
}
