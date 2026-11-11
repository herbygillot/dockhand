package sweep

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/command/cmdtest"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/forge/forgetest"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// started is a World with jq's branch started and nothing in it yet.
func started(t *testing.T, w cmdtest.World) *forgetest.GitHub {
	t.Helper()
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil
	dockhand(t, "start", "jq-update")
	return g
}

// edited is started with jq updated in it, not yet tidied.
func edited(t *testing.T, w cmdtest.World) *forgetest.GitHub {
	t.Helper()
	g := started(t, w)
	dockhand(t, "update", "jq", "-b", "jq-update")
	return g
}

func dockhand(t *testing.T, args ...string) {
	t.Helper()
	_, errs, err := cmdtest.Dockhand(t, args...)
	require.NoError(t, err, "%v: %s", args, errs)
}

// submitted is tidied with its pull request opened.
func submitted(t *testing.T, w cmdtest.World) *forgetest.GitHub {
	t.Helper()
	g := tidied(t, w)
	dockhand(t, "submit", "-b", "jq-update", "--no-check", "--yes")
	return g
}

// tidied is jq updated in its own branch and tidied, the GitHub stand-in
// knowing of no other pull request.
func tidied(t *testing.T, w cmdtest.World) *forgetest.GitHub {
	t.Helper()
	cmdtest.Versioned(t, w)
	cmdtest.WithBumper(t)
	cmdtest.WithScript(t, w, "passed")
	g := cmdtest.WithGitHub(t, w)
	g.Others = nil
	for _, args := range [][]string{{"start", "jq-update"}, {"update", "jq", "-b", "jq-update"}, {"tidy", "-b", "jq-update", "-y"}} {
		_, errs, err := cmdtest.Dockhand(t, args...)
		require.NoError(t, err, "%v: %s", args, errs)
	}
	return g
}

// The first submit of a tidied branch: its push, its pull request, and
// the records of both.
func TestSweepFirstSubmit(t *testing.T) {
	t.Parallel()
	sweep(t, scenario{
		name:    "first submit",
		arrange: tidied,
		steps:   [][]string{{"submit", "-b", "jq-update", "--no-check", "--yes"}},
		bestEffort: map[string]string{
			"store.update@engine.(*Engine).PlanSubmit#":     "its note of what took the network to find is a saving, which submit says (submit.go)",
			"git update-ref@engine.(*Engine).fetchMaster#2": "the master kept for status is a saving: a ref that can't be written leaves the fetch as it was (fetchMaster)",
		},
	})
}

// A resubmit after a reviewer asked for changes: a new commit pushed, the
// description refreshed, and the reviewer asked again, which #117 lost
// where the pull request's update failed.
func TestSweepResubmitAfterReview(t *testing.T) {
	t.Parallel()
	sweep(t, scenario{
		name: "resubmit after review",
		arrange: func(t *testing.T, w cmdtest.World) *forgetest.GitHub {
			g := submitted(t, w)
			g.Status = forge.PullRequestStatus{Review: "changes-requested", ChangesRequested: 1, ChangesRequestedBy: []string{"ryandesign"}}
			dockhand(t, "status", "--refresh", "-b", "jq-update")
			config := filepath.Join(w.Home, ".dockhand", "config.toml")
			data, err := os.ReadFile(config)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(config, append(data, "[submit]\nrerequest_review = \"always\"\n"...), 0o644))
			// The reviewer gets their review in: a new commit, as a person
			// makes it.
			worktree := filepath.Join(w.Home, "Source", "macports-branches", "jq-update")
			require.NoError(t, os.WriteFile(filepath.Join(worktree, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n# drop the patch\n"), 0o644))
			testsupport.Git(t, worktree, "commit", "-q", "-am", "jq: drop the patch")
			return g
		},
		steps: [][]string{{"submit", "-b", "jq-update", "--no-check", "--yes"}},
		bestEffort: map[string]string{
			"store.update@engine.(*Engine).PlanSubmit#":     "its note of what took the network to find is a saving, which submit says (submit.go)",
			"git update-ref@engine.(*Engine).fetchMaster#2": "the master kept for status is a saving (fetchMaster)",
		},
		known: map[string]string{
			// The sweep's first run (#129): the reviewer asked again only on
			// the run that pushed, so a submit that failed after its push, or
			// whose ask failed, never asks again; #117's family.
			"store.update@engine.(*Engine).ApplySubmit#1/":                  "#205: the reviewer isn't asked again after the submit's record fails",
			"git push@remotewrite.Push#1/lost":                              "#205: the reviewer isn't asked again after the push's reply is lost",
			"forge.Update@remotewrite.Edit#1/":                              "#205: the reviewer isn't asked again after the pull request's edit fails",
			"forge.RequestReviewers@remotewrite.Ask#1/":                     "#205: a failed ask of the reviewer isn't made again",
			"store.update@engine.(*Engine).RequestReview#1/!landed-unknown": "#206: the ask made, its record failed, the timeline never says it",
		},
	})
}

// An update of jq in its branch: the Portfile edited, and the edit
// recorded, which the scratch run found left unrecorded where the record
// failed (#194's agreement case).
func TestSweepUpdate(t *testing.T) {
	t.Parallel()
	sweep(t, scenario{name: "update", arrange: started, steps: [][]string{{"update", "jq", "-b", "jq-update"}},
		known: map[string]string{
			// The edit made, its record failed: update again says jq is
			// already at 1.8.1, and tidy calls the edit foreign.
			"store.update@engine.(*Engine).update#1/!landed-unknown": "#194's agreement case: the edit is never recorded",
		}})
}

// A tidy of an updated branch: its commit made, its checkpoint and the
// index left as the commit has them.
func TestSweepTidy(t *testing.T) {
	t.Parallel()
	sweep(t, scenario{name: "tidy", arrange: edited, steps: [][]string{{"tidy", "-b", "jq-update", "-y"}}})
}

// An undo of a tidy: the history put back, the edits uncommitted again,
// and the checkpoint marked restored (#113).
func TestSweepUndo(t *testing.T) {
	t.Parallel()
	sweep(t, scenario{name: "undo", arrange: tidied, steps: [][]string{{"undo", "-b", "jq-update"}},
		known: map[string]string{
			"git update-index@engine.(*Engine).restore#1/":                        "#209: undo half done refuses to finish, and records no restore",
			"git update-ref@engine.(*Engine).restore#1/landed-unknown":            "#209: undo half done refuses to finish, and records no restore",
			"store.update@history.(*Transitions).RecordRestore#1/!landed-unknown": "#210: the history restored, its record failed: undo again refuses, and the timeline never says it",
		}})
}

// A clean after the pull request merged: the branch, its worktree and its
// fork branch removed, and the record ended.
func TestSweepCleanAfterMerge(t *testing.T) {
	t.Parallel()
	sweep(t, scenario{
		name: "clean after merge",
		arrange: func(t *testing.T, w cmdtest.World) *forgetest.GitHub {
			g := submitted(t, w)
			g.PRs[34901].State = forge.PullRequestMerged
			dockhand(t, "status", "--refresh", "-b", "jq-update")
			return g
		},
		steps: [][]string{{"clean", "--yes"}},
		bestEffort: map[string]string{
			"store.update@coord.(*Session).End#":                 "a session's end is best-effort: a session not ended is judged by its process (coord)",
			"store.update@engine.(*Engine).RefreshPullRequests#": "clean's refresh of the pull request's record is a saving; the merge is read from GitHub again",
		},
		known: map[string]string{
			"store.update@coord.(*Session).End#1/!landed-unknown":        "#211: the timeline loses clean's event where its session's end fails",
			"store.update@engine.(*Engine).keepCleanWords#1/dead":        "#211: the timeline loses clean's event where its record fails",
			"store.update@engine.(*Engine).ApplyClean#1/!landed-unknown": "#211: the timeline loses clean's event where its record fails",
			"git update-ref@engine.(*Engine).ApplyClean#1/":              "#211: the timeline loses clean's event where a ref's removal fails",
			"git push@remotewrite.Delete#1/":                             "#212: a fork branch's failed removal leaves the branch's checkpoints, or clean's event, behind",
			"git update-ref@engine.(*Engine).dropRefs#1/":                "#212: the branch's checkpoint refs are left behind where their removal fails",
		},
	})
}
