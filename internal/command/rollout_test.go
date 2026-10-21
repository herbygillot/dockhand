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

// status, path, archive, and the run commands take -b, -p, and --pr, the
// run commands meaning the branch's latest check, from anywhere (the
// second look's §3).
func TestTheSelectorReachesStatusPathArchiveAndRuns(t *testing.T) {
	w := checkedBranch(t)
	_, _, err := dockhand(t, "check")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", w.clone)

	out, _, err := dockhand(t, "status", "-p", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · ~/Source/macports-branches/jq-update\n", "one branch touches jq: shown in detail")
	out, _, err = dockhand(t, "status", "-b", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · ")
	_, _, err = dockhand(t, "status", "jq-update", "-b", "jq-update")
	require.EqualError(t, err, "name the branch once: as the argument, or with -b, -p, or --pr")

	out, _, err = dockhand(t, "path", "-p", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "macports-branches/jq-update\n")

	out, _, err = dockhand(t, "logs", "-p", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "building from ", "jq's log, from its branch's latest check")
	out, _, err = dockhand(t, "logs", "-b", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, "check-1 · passed\n")
	out, _, err = dockhand(t, "wait", "-p", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "Passed for snapshot 1.\n", "check-1, reported")
	_, _, err = dockhand(t, "wait", "check-1", "-p", "jq")
	require.EqualError(t, err, "name a check or a branch, not both")
	_, _, err = dockhand(t, "cancel", "-b", "jq-update")
	require.EqualError(t, err, "check-1 already passed")

	out, _, err = dockhand(t, "archive", "-p", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "Archived jq-update")
}

// check in a checkout dockhand doesn't track offers what tracks it, on a
// terminal, where nothing else could be meant: a Git branch someone made
// is adopted, and a detached HEAD gets a branch for the port it changes.
// A script is refused, naming the command (the command-line UX review's
// §2).
func TestCheckOffersToTrackAnUntrackedCheckout(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	t.Setenv("MACPORTS_TREE", w.clone)
	testsupport.Git(t, w.clone, "switch", "-q", "-c", "my-jq")
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\n# mine\n"), 0o644))
	testsupport.Git(t, w.clone, "commit", "-q", "-am", "jq: mine")

	_, _, err := dockhand(t, "check", "--plan")
	require.ErrorContains(t, err, "dockhand adopt my-jq tracks it, then run this again")
	var out, errs bytes.Buffer
	err = runFor(t, t.Context(), []string{"check", "--plan"}, Streams{In: strings.NewReader("y\n"), Out: &out, Err: &errs, interactive: true})
	require.NoError(t, err, errs.String())
	require.Contains(t, errs.String(), "? my-jq isn't tracked; track it with adopt? [y/N] ")
	require.Contains(t, errs.String(), "Tracking my-jq.\n")

	// A commit of jq's, on no branch.
	testsupport.Git(t, w.clone, "switch", "-q", "--detach")
	require.NoError(t, os.WriteFile(filepath.Join(w.clone, "textproc/jq/Portfile"), []byte("name jq\nversion 1.7.1\n# edited\n"), 0o644))
	testsupport.Git(t, w.clone, "commit", "-q", "-am", "jq: edited")
	_, _, err = dockhand(t, "check", "--plan")
	require.ErrorContains(t, err, "git switch -c dockhand/<name> and dockhand adopt")
	out.Reset()
	errs.Reset()
	err = runFor(t, t.Context(), []string{"check", "--plan"}, Streams{In: strings.NewReader("y\n"), Out: &out, Err: &errs, interactive: true})
	require.NoError(t, err, errs.String())
	require.Regexp(t, `Started jq-[a-z0-9]{4} at this commit, and tracking it\.\n`, errs.String(), "named for its port")
	require.Regexp(t, `^dockhand/jq-[a-z0-9]{4}$`, testsupport.Git(t, w.clone, "branch", "--show-current"))
}

// A new branch that asks for an archived branch's name is offered it, on a
// terminal: the archived one is renamed aside, <name>-archived, its Git
// branch and record, which archive --undo then finds; a script is told so,
// and an open branch's name says how to work in it (decided 2026-10-07;
// the rc8 full stage's B10).
func TestStartOffersAnArchivedBranchsName(t *testing.T) {
	w := newWorld(t)
	_, _, err := dockhand(t, "start", "notes")
	require.NoError(t, err)
	_, _, err = dockhand(t, "start", "notes")
	require.EqualError(t, err, `dockhand/notes is already a tracked branch, open: cd "$(dockhand path notes)" works in it, or choose another name`)
	_, _, err = dockhand(t, "archive", "notes")
	require.NoError(t, err)

	_, _, err = dockhand(t, "start", "notes")
	require.EqualError(t, err, "dockhand/notes is an archived branch, whose record keeps the name; on a terminal start offers to rename it notes-archived, or choose another name")
	var out, errs bytes.Buffer
	err = runFor(t, t.Context(), []string{"start", "notes"}, Streams{In: strings.NewReader("y\n"), Out: &out, Err: &errs, interactive: true})
	require.NoError(t, err, errs.String())
	require.Contains(t, errs.String(), "? notes is archived. Rename the archived branch to free this name? [y/N] ")
	require.Contains(t, out.String(), "Renamed the archived notes to notes-archived; dockhand archive --undo notes-archived brings it back.\n")
	require.Contains(t, out.String(), "Created dockhand/notes from master ")
	require.Contains(t, testsupport.Git(t, w.clone, "branch", "--list", "dockhand/notes-archived"), "dockhand/notes-archived")

	undone, _, err := dockhand(t, "archive", "--undo", "notes-archived")
	require.NoError(t, err)
	require.Contains(t, undone, "notes-archived is back among your open branches.\n")
}

// A branch renamed with Git onto a name an archived record holds, its own
// Git branch gone, is followed: the archived record is renamed aside, and
// status goes on, where every status failed on the name's uniqueness (the
// rc1 full stage, D-S5).
func TestStatusFollowsARenameOntoAnArchivedBranchsName(t *testing.T) {
	w := newWorld(t)
	_, _, err := dockhand(t, "start", "ds5-after")
	require.NoError(t, err)
	_, _, err = dockhand(t, "archive", "ds5-after")
	require.NoError(t, err)
	testsupport.Git(t, w.clone, "branch", "-q", "-D", "dockhand/ds5-after")
	_, _, err = dockhand(t, "start", "ds5-before")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "ds5-before")
	testsupport.Git(t, dir, "branch", "-m", "dockhand/ds5-before", "dockhand/ds5-after")

	t.Setenv("MACPORTS_TREE", w.clone)
	out, errs, err := dockhand(t, "status")
	require.NoError(t, err, errs)
	require.Contains(t, out, "Followed ds5-before, renamed by hand to ds5-after.\n")
	out, _, err = dockhand(t, "status", "--all")
	require.NoError(t, err)
	require.Contains(t, out, "ds5-after-archived (archived)", "the archived record, renamed aside")
	_, _, err = dockhand(t, "--json", "status")
	require.NoError(t, err)
}
