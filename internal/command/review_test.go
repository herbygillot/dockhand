package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

func TestTidyAppliesDockhandsOwnEditsAndRestoreUndoesIt(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")
	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)

	out, _, err := dockhand(t, "tidy", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · edits not yet committed\n\nProposed commit\n  1  jq: update to 1.8.1\n       includes edits not yet committed\n       files: textproc/jq/Portfile\n")
	require.Equal(t, "jq: 1.7.1", testsupport.Git(t, dir, "log", "-1", "--format=%s"), "a plan changes nothing")

	out, _, err = dockhand(t, "tidy")
	require.NoError(t, err, "a script applies an unambiguous plan")
	require.Contains(t, out, "Created 1 commit. The files are unchanged.\nCheckpoint tidy-1 keeps the old history (dockhand restore tidy-1).\n")
	require.Equal(t, "jq: update to 1.8.1", testsupport.Git(t, dir, "log", "-1", "--format=%s"))

	out, _, err = dockhand(t, "tidy")
	require.NoError(t, err)
	require.Contains(t, out, "nothing to tidy")

	out, _, err = dockhand(t, "restore", "tidy-1")
	require.NoError(t, err)
	require.Contains(t, out, "Restored dockhand/jq-update to its history before tidy-1")
	require.Equal(t, "M textproc/jq/Portfile", testsupport.Git(t, dir, "status", "--porcelain"))
}

func TestTidyAsksAboutAPersonsCommits(t *testing.T) {
	w := newWorld(t)
	withBumper(t)
	testsupport.Git(t, w.clone, "switch", "-q", "-c", "update-jq")
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "textproc/jq/Portfile"), []byte("name jq\n# a\n"), 0o644))
	testsupport.Git(t, w.clone, "commit", "-q", "-am", "wip")
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "textproc/jq/Portfile"), []byte("name jq\n# b\n"), 0o644))
	testsupport.Git(t, w.clone, "commit", "-q", "-am", "oops")
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
	require.Equal(t, "jq: describe the b option", testsupport.Git(t, w.clone, "log", "-1", "--format=%s"))

	_, _, err = dockhand(t, "tidy", "--message", "x")
	require.ErrorContains(t, err, "add --squash")
}

// A plan tidy proposes of a person's own edits, which needs a review on a
// terminal, applies as shown with --yes where nothing holds it, and isn't
// applied where something does: qemu's hand edit had no way to be taken
// without a terminal (field testing's seventh report, 2026-10-02).
func TestTidyYesAppliesAPersonsPlanAsShown(t *testing.T) {
	w := newWorld(t)
	withBumper(t)
	testsupport.Git(t, w.clone, "switch", "-q", "-c", "update-jq")
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "textproc/jq/Portfile"), []byte("name jq\n# a\n"), 0o644))
	testsupport.Git(t, w.clone, "commit", "-q", "-am", "wip")
	_, _, err := dockhand(t, "adopt")
	require.NoError(t, err)

	_, _, err = dockhand(t, "tidy", "--yes")
	require.ErrorContains(t, err, "nothing was applied: the commit for textproc/jq needs a subject")
	_, _, err = dockhand(t, "tidy")
	require.ErrorContains(t, err, "apply it as shown with --yes")

	testsupport.Git(t, w.clone, "commit", "-q", "--amend", "-m", "jq: note a")
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "textproc/jq/Portfile"), []byte("name jq\n# a, and b\n"), 0o644))
	out, _, err := dockhand(t, "tidy", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "Created 1 commit.")
	require.Equal(t, "jq: note a", testsupport.Git(t, w.clone, "log", "-1", "--format=%s"))
	require.Empty(t, testsupport.Git(t, w.clone, "status", "--porcelain"))
}

func TestTidyRegroupsAndAppliesASavedPlan(t *testing.T) {
	w := newWorld(t)
	testsupport.Git(t, w.clone, "switch", "-q", "-c", "harbor")
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "_resources/port1.0/group/github-1.0.tcl"), []byte("# group, for harbor\n"), 0o644))
	testsupport.Git(t, w.clone, "commit", "-q", "-am", "github-1.0: follow harbor's releases")
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "textproc/jq/Portfile"), []byte("name jq\n# harbor\n"), 0o644))
	testsupport.Git(t, w.clone, "commit", "-q", "-am", "jq: note harbor")
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "textproc/jq/Portfile"), []byte("name jq\n# harbor, uncommitted\n"), 0o644))
	_, _, err := dockhand(t, "adopt")
	require.NoError(t, err)

	file := filepath.Join(t.TempDir(), "plan.toml")
	_, _, err = dockhand(t, "tidy", "--out", file)
	require.ErrorContains(t, err, "add --plan")
	out, _, err := dockhand(t, "tidy", "--plan", "--group", "2 1", "--out", file)
	require.NoError(t, err)
	require.Contains(t, out, "  1  jq: note harbor\n")
	require.Contains(t, out, "  2  github-1.0: follow harbor's releases\n")
	require.Contains(t, out, "Saved the plan to "+file+".")
	require.Equal(t, "jq: note harbor", testsupport.Git(t, w.clone, "log", "-1", "--format=%s"), "saving changes nothing")

	// A message reads in the saved plan as the commit will say it, and is
	// edited as plain text (the hugo exercise's re-submitting sshuttle,
	// finding 1).
	saved, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Contains(t, string(saved), "message = '''\njq: note harbor\n'''\n")
	edited := strings.Replace(string(saved), "message = '''\njq: note harbor\n'''", "message = '''\njq: note harbor\n\nThe note says why harbor needs jq.\n\nIt names the build.\n'''", 1)
	require.NoError(t, os.WriteFile(file, []byte(edited), 0o644))

	out, _, err = dockhand(t, "tidy", "--apply", file)
	require.NoError(t, err)
	require.Contains(t, out, "harbor · the plan saved in "+file+"\n")
	// It shows each message as it will be written, the edit included, and
	// not the notes on how the proposal was made (finding 2).
	require.Contains(t, out, "\nCommits to write\n  1  jq: note harbor\n       includes edits not yet committed\n       files: textproc/jq/Portfile\n       author: Test <test@example.org>\n       body:\n         The note says why harbor needs jq.\n\n         It names the build.\n  2  github-1.0: follow harbor's releases\n")
	require.NotContains(t, out, "subject from")
	require.Contains(t, out, "Created 2 commits.")
	require.Equal(t, "github-1.0: follow harbor's releases\njq: note harbor", testsupport.Git(t, w.clone, "log", "-2", "--format=%s"))
	require.Equal(t, "jq: note harbor\n\nThe note says why harbor needs jq.\n\nIt names the build.", testsupport.Git(t, w.clone, "log", "-1", "--format=%B", "HEAD~1"))
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
	require.Equal(t, "github-1.0: follow harbor's releases", testsupport.Git(t, w.clone, "log", "-1", "--format=%s"))
	require.Equal(t, "_resources/port1.0/group/github-1.0.tcl\ntextproc/jq/Portfile", testsupport.Git(t, w.clone, "show", "--format=", "--name-only", "HEAD"))
}

// What MacPorts' rules warn of in commits tidy would keep is said, as
// submit says it, with the commits saved as they are to rewrite a message
// in: tidy said "nothing to tidy" where submit warned of a body line over
// 72 characters, and saved no plan to fix it (the rust and cargo run).
func TestTidySaysWhatTheRulesWarnOfAndSavesTheCommitsToRewrite(t *testing.T) {
	w := newWorld(t)
	testsupport.Git(t, w.clone, "switch", "-q", "-c", "update-jq")
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "textproc/jq/Portfile"), []byte("name jq\n# a\n"), 0o644))
	long := "This body line runs on past the seventy-two characters MacPorts asks of it."
	testsupport.Git(t, w.clone, "commit", "-q", "-am", "jq: note a\n\n"+long)
	_, _, err := dockhand(t, "adopt")
	require.NoError(t, err)
	commit := testsupport.Git(t, w.clone, "rev-parse", "--short=7", "HEAD")

	out, _, err := dockhand(t, "tidy", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "The commits follow MacPorts' rules, and nothing is uncommitted, but for what the rules warn of:\n  ! commit "+commit+": body has lines over 72 characters [body-wrap]\n")
	require.Contains(t, out, "To rewrite a message: dockhand tidy --plan --out tidy.toml")

	file := filepath.Join(t.TempDir(), "tidy.toml")
	out, _, err = dockhand(t, "tidy", "--plan", "--out", file)
	require.NoError(t, err)
	require.Contains(t, out, "Saved the commits as they are to "+file+".")
	saved, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Contains(t, string(saved), long)
	edited := strings.Replace(string(saved), long, "This body line now wraps at the seventy-two characters\nMacPorts asks of it.", 1)
	require.NoError(t, os.WriteFile(file, []byte(edited), 0o644))
	_, _, err = dockhand(t, "tidy", "--apply", file)
	require.NoError(t, err)
	require.Equal(t, "jq: note a\n\nThis body line now wraps at the seventy-two characters\nMacPorts asks of it.", testsupport.Git(t, w.clone, "log", "-1", "--format=%B"))
	out, _, err = dockhand(t, "tidy", "--plan")
	require.NoError(t, err)
	require.Contains(t, out, "nothing to tidy", "with nothing left to warn of")
}
