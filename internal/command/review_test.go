package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTidyAppliesDockhandsOwnEditsAndRestoreUndoesIt(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "src", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)

	out, _, err := dockhand(t, "tidy", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · edits not yet committed\n\nProposed commit\n  1  jq: update to 1.8.1\n       includes edits not yet committed\n       files: textproc/jq/Portfile\n")
	require.Equal(t, "jq: 1.7.1", gitRun(t, dir, "log", "-1", "--format=%s"), "a plan changes nothing")

	out, _, err = dockhand(t, "tidy")
	require.NoError(t, err, "a script applies an unambiguous plan")
	require.Contains(t, out, "Created 1 commit. The files are unchanged.\nCheckpoint tidy-1 keeps the old history (dockhand restore tidy-1).\n")
	require.Equal(t, "jq: update to 1.8.1", gitRun(t, dir, "log", "-1", "--format=%s"))

	out, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	require.Contains(t, out, "nothing to tidy")

	out, _, err = dockhand(t, "restore", "tidy-1")
	require.NoError(t, err)
	require.Contains(t, out, "Restored dockhand/jq-update to its history before tidy-1")
	require.Equal(t, "M textproc/jq/Portfile", gitRun(t, dir, "status", "--porcelain"))
}

func TestTidyAsksAboutAPersonsCommits(t *testing.T) {
	w := newWorld(t)
	withBumper(t)
	gitRun(t, w.clone, "switch", "-q", "-c", "update-jq")
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "textproc/jq/Portfile"), []byte("name jq\n# a\n"), 0o644))
	gitRun(t, w.clone, "commit", "-q", "-am", "wip")
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "textproc/jq/Portfile"), []byte("name jq\n# b\n"), 0o644))
	gitRun(t, w.clone, "commit", "-q", "-am", "oops")
	_, _, err := dockhand(t, "adopt")
	require.NoError(t, err)

	_, _, err = dockhand(t, "tidy")
	require.ErrorContains(t, err, "needs review before it is applied")

	var out, errs bytes.Buffer
	input := "a\ne\njq: describe the b option\nd\na\n"
	err = Run(t.Context(), []string{"tidy"}, Streams{In: strings.NewReader(input), Out: &out, Err: &errs, interactive: true})
	require.NoError(t, err)
	require.Contains(t, out.String(), "  1  (needs a subject)\n       combines \"wip\", \"oops\"\n")
	require.Contains(t, errs.String(), "Not yet: the commit for textproc/jq needs a subject")
	require.Contains(t, out.String(), "  1  jq: describe the b option\n")
	require.Contains(t, out.String(), "+# b", "the diff was shown")
	require.Contains(t, out.String(), "Created 1 commit.")
	require.Equal(t, "jq: describe the b option", gitRun(t, w.clone, "log", "-1", "--format=%s"))

	_, _, err = dockhand(t, "tidy", "--message", "x")
	require.ErrorContains(t, err, "add --squash")
}

func TestTidyRegroupsAndAppliesASavedPlan(t *testing.T) {
	w := newWorld(t)
	gitRun(t, w.clone, "switch", "-q", "-c", "harbor")
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "_resources/port1.0/group/github-1.0.tcl"), []byte("# group, for harbor\n"), 0o644))
	gitRun(t, w.clone, "commit", "-q", "-am", "github-1.0: follow harbor's releases")
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "textproc/jq/Portfile"), []byte("name jq\n# harbor\n"), 0o644))
	gitRun(t, w.clone, "commit", "-q", "-am", "jq: note harbor")
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "textproc/jq/Portfile"), []byte("name jq\n# harbor, uncommitted\n"), 0o644))
	_, _, err := dockhand(t, "adopt")
	require.NoError(t, err)

	file := filepath.Join(t.TempDir(), "plan.json")
	_, _, err = dockhand(t, "tidy", "--out", file)
	require.ErrorContains(t, err, "add --plan")
	out, _, err := dockhand(t, "tidy", "--plan", "--group", "2 1", "--out", file)
	require.NoError(t, err)
	require.Contains(t, out, "  1  jq: note harbor\n")
	require.Contains(t, out, "  2  github-1.0: follow harbor's releases\n")
	require.Contains(t, out, "Saved the plan to "+file+".")
	require.Equal(t, "jq: note harbor", gitRun(t, w.clone, "log", "-1", "--format=%s"), "saving changes nothing")

	out, _, err = dockhand(t, "tidy", "--apply", file)
	require.NoError(t, err)
	require.Contains(t, out, "harbor · the plan saved in "+file+"\n")
	require.Contains(t, out, "Created 2 commits.")
	require.Equal(t, "github-1.0: follow harbor's releases\njq: note harbor", gitRun(t, w.clone, "log", "-2", "--format=%s"))
	_, _, err = dockhand(t, "tidy", "--apply", file)
	require.ErrorContains(t, err, "it has new commits")

	_, _, err = dockhand(t, "restore", "tidy-1")
	require.NoError(t, err)
	var buffer, errs bytes.Buffer
	err = Run(t.Context(), []string{"tidy"}, Streams{In: strings.NewReader("g\n1+3\ng\n1+2\na\n"), Out: &buffer, Err: &errs, interactive: true})
	require.NoError(t, err)
	require.Contains(t, errs.String(), "Review diff [d] · Change groups [g] · Edit message [e] · Apply [a] · Cancel [q]")
	require.Contains(t, errs.String(), `Not changed: "3" is not one of the commits, 1 to 2`)
	require.Contains(t, buffer.String(), "combines commits 1, 2; message from commit 1, so check it says what all of them do")
	require.Contains(t, buffer.String(), "Created 1 commit.")
	require.Equal(t, "github-1.0: follow harbor's releases", gitRun(t, w.clone, "log", "-1", "--format=%s"))
	require.Equal(t, "_resources/port1.0/group/github-1.0.tcl\ntextproc/jq/Portfile", gitRun(t, w.clone, "show", "--format=", "--name-only", "HEAD"))
}
