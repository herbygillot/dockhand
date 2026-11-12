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
		unreadable: map[string]declaredEnd{
			"store.update@engine.(*Engine).rewritePublication#1/fail": {Replace: []string{`pr_provenance="found"`, `pr_provenance="opened"`},
				Why: "the opening's record failed: the next submit finds the pull request, and GitHub's answer to the create, the only proof dockhand opened it, is gone, so it is found (#191)"},
			// The same state, reached in the other modes, each declared for
			// itself (#129's fix list, 9).
			"store.update@engine.(*Engine).rewritePublication#1/not-landed-unknown": {Replace: []string{`pr_provenance="found"`, `pr_provenance="opened"`},
				Why: "the opening's record didn't land and said nothing: the next submit finds the pull request, GitHub's answer to the create gone, so it is found (#191)"},
			"store.update@engine.(*Engine).rewritePublication#1/dead": {Replace: []string{`pr_provenance="found"`, `pr_provenance="opened"`},
				Why: "submit was killed before the opening's record: the next submit finds the pull request, GitHub's answer to the create gone with the process, so it is found (#191)"},
			"forge.Create@remotewrite.Open#1/lost": {Replace: []string{`pr_provenance="found"`, `pr_provenance="opened"`},
				Why: "GitHub opened the pull request and its answer was lost: nothing proves dockhand opened it, and the next submit finds it, so it is found (#191)"},
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
		unreadable: map[string]declaredEnd{
			"store.update@engine.(*Engine).rewritePublication#3/fail": {Replace: []string{`\"Sent\":\"may-have-been-sent\"`, `\"Sent\":\"sent\"`},
				Why: "the ask's answer wasn't recorded: the mark recorded before the call says it may have been sent, and it is never sent again (#191's case AC)"},
			"forge.RequestReviewers@remotewrite.Ask#1/fail": {Replace: []string{`\"Sent\":\"may-have-been-sent\"`, `\"Sent\":\"sent\"`, "rerequested=[]", "rerequested=[ryandesign]"},
				Unsaid: []string{`kind="branch.rerequest"`},
				Why:    "an ask with no answer may have landed, so it is never sent again: GitHub may lack the request, and the receipt says it may have been sent rather than the timeline that it was (#191's case AC)"},
			// The same states, reached in the other modes, each declared
			// for itself (#129's fix list, 9).
			"store.update@engine.(*Engine).rewritePublication#3/not-landed-unknown": {Replace: []string{`\"Sent\":\"may-have-been-sent\"`, `\"Sent\":\"sent\"`},
				Why: "the ask's answer didn't land and said nothing: the mark recorded before the call says it may have been sent, and it is never sent again (#191's case AC)"},
			"store.update@engine.(*Engine).rewritePublication#3/dead": {Replace: []string{`\"Sent\":\"may-have-been-sent\"`, `\"Sent\":\"sent\"`},
				Why: "submit was killed before the ask's answer was recorded: the mark recorded before the call says it may have been sent, and it is never sent again (#191's case AC)"},
			"forge.RequestReviewers@remotewrite.Ask#1/lost": {Replace: []string{`\"Sent\":\"may-have-been-sent\"`, `\"Sent\":\"sent\"`},
				Unsaid: []string{`kind="branch.rerequest"`},
				Why:    "GitHub took the ask and its answer was lost: it may have been sent, so it is never sent again, and the receipt says so rather than the timeline that it was (#191's case AC)"},
			"forge.RequestReviewers@remotewrite.Ask#1/dead": {Replace: []string{`\"Sent\":\"may-have-been-sent\"`, `\"Sent\":\"sent\"`, "rerequested=[]", "rerequested=[ryandesign]"},
				Unsaid: []string{`kind="branch.rerequest"`},
				Why:    "submit was killed as it asked, after the mark: the ask may have landed, so it is never sent again, GitHub may lack it, and the receipt says it may have been sent (#191's case AC)"},
		},
		// The sweep's first run found the reviewer asked again only on the
		// run that pushed (#205, #206, #117's family): the publication
		// record carries the request to the next submit of the commit.
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
	sweep(t, scenario{name: "undo", arrange: tidied, steps: [][]string{{"undo", "-b", "jq-update"}}})
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
			// Clean's event can't be recorded where its own record fails:
			// what it removed is gone, and the next clean finds nothing.
			"store.update@engine.(*Engine).cleanEvent#1/!landed-unknown": "#211: the timeline loses clean's event where that record fails",
			// A removal that happened and wasn't seen to, or a stop before
			// the event: the next clean finds it gone, and says nothing of
			// what the earlier one removed.
			"git push@remotewrite.Delete#1/lost":                          "#211: the event leaves out a fork branch removed whose reply was lost",
			"git update-ref@engine.(*Engine).ApplyClean#1/landed-unknown": "#211: the event leaves out a branch removed whose outcome was unknown",
			"git push@remotewrite.Delete#1/dead":                          "#211: a stop after a removal loses the event of what was removed",
			"git update-ref@engine.(*Engine).ApplyClean#1/dead":           "#211: a stop after a removal loses the event of what was removed",
			"git update-ref@engine.(*Engine).dropRefs#1/dead":             "#211: a stop after a removal loses the event of what was removed",
		},
	})
}
