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
	err = Run(t.Context(), []string{"check", "--plan"}, Streams{In: strings.NewReader("y\n"), Out: &out, Err: &errs, interactive: true})
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
	err = Run(t.Context(), []string{"check", "--plan"}, Streams{In: strings.NewReader("y\n"), Out: &out, Err: &errs, interactive: true})
	require.NoError(t, err, errs.String())
	require.Regexp(t, `Started jq-[a-z0-9]{4} at this commit, and tracking it\.\n`, errs.String(), "named for its port")
	require.Regexp(t, `^dockhand/jq-[a-z0-9]{4}$`, testsupport.Git(t, w.clone, "branch", "--show-current"))
}
