package engine

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// Settling, one table: a publication and what each read answers, complete
// or failed, against what is folded, what stays owed, and whether it ends
// (docs/designs/publication-record.md, "Tests", and cases A, AN, D, F, O,
// R, U).
func TestSettlingReadsWhatEachStepLeft(t *testing.T) {
	t.Parallel()
	commit, expected, other := model.ObjectID(strings.Repeat("c", 40)), model.ObjectID(strings.Repeat("e", 40)), model.ObjectID(strings.Repeat("f", 40))
	body := "#### Description\n\njq: update to 1.8\n\n###### Tested on\n\nevidence\n"
	opening := model.Publication{Commit: commit, Expected: expected, Earlier: 40, Title: "jq: update to 1.8", Body: body, Written: body,
		Owes: []model.PublicationStep{model.StepPush, model.StepOpen, model.StepReady}}
	found := func(number int, state forge.PullRequestState, text string) forge.PullRequest {
		return forge.PullRequest{Ref: forge.PullRequestRef{Number: number}, State: state, Title: "jq: update to 1.8", Body: text}
	}
	read := PublicationReading{HeadRead: true, HeadExists: true, HeadAt: commit, FoundRead: true, Found: true, PullRequest: found(41, forge.PullRequestOpen, strings.ReplaceAll(body, "\n", "\r\n")), StatusRead: true, Status: forge.PullRequestStatus{Draft: true}}
	for _, c := range []struct {
		name    string
		reading func(PublicationReading) PublicationReading
		want    Settlement
	}{
		{"A. the create's reply was lost: the push and the pull request are there, found", func(r PublicationReading) PublicationReading { return r },
			Settlement{Done: []model.PublicationStep{model.StepPush, model.StepOpen}, Owes: []model.PublicationStep{model.StepReady}, Number: 41, Provenance: model.Found, Body: body}},
		{"F. --ready asked and not yet made", func(r PublicationReading) PublicationReading { return r },
			Settlement{Done: []model.PublicationStep{model.StepPush, model.StepOpen}, Owes: []model.PublicationStep{model.StepReady}, Number: 41, Provenance: model.Found, Body: body}},
		{"nothing pushed yet: the branch is where the plan saw it", func(r PublicationReading) PublicationReading { r.HeadAt, r.Found = expected, false; return r },
			Settlement{Owes: []model.PublicationStep{model.StepPush, model.StepOpen, model.StepReady}}},
		{"AN. only an earlier pull request is found, which settles nothing", func(r PublicationReading) PublicationReading {
			r.PullRequest = found(40, forge.PullRequestClosed, "")
			return r
		}, Settlement{Done: []model.PublicationStep{model.StepPush}, Owes: []model.PublicationStep{model.StepOpen, model.StepReady}}},
		{"O. someone moved the fork's branch: what landed is folded, and it ends", func(r PublicationReading) PublicationReading { r.HeadAt = other; return r },
			Settlement{Done: []model.PublicationStep{model.StepOpen}, Number: 41, Provenance: model.Found, Body: body, Ended: true, Why: "the fork's branch is at fffffff, which this submission didn't push"}},
		{"R. merged in between: folded, then ended", func(r PublicationReading) PublicationReading {
			r.PullRequest = found(41, forge.PullRequestMerged, body)
			return r
		},
			Settlement{Done: []model.PublicationStep{model.StepPush, model.StepOpen}, Number: 41, Provenance: model.Found, Body: body, Ended: true, Why: "#41 was merged"}},
		{"U. GitHub can't be read: nothing is settled, and each read is named", func(r PublicationReading) PublicationReading {
			r.FoundRead, r.StatusRead = false, false
			return r
		}, Settlement{Done: []model.PublicationStep{model.StepPush}, Owes: []model.PublicationStep{model.StepOpen, model.StepReady}, Unread: []string{"the pull request", "the pull request's draft state"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, c.want, Settle(opening, nil, c.reading(read)))
		})
	}

	// D. An update whose record failed: the edit landed, part by part
	// where a person edited the Description since.
	update := model.Publication{Commit: commit, Expected: expected, Number: 41, Title: "jq: update to 1.8", Body: body, Written: body, Owes: []model.PublicationStep{model.StepPush, model.StepEdit}}
	recorded := &model.PullRequest{Number: 41, Body: "#### Description\n\njq: update to 1.7\n\n###### Tested on\n\nold\n"}
	settled := Settle(update, recorded, read)
	require.Equal(t, []model.PublicationStep{model.StepPush, model.StepEdit}, settled.Done)
	require.Empty(t, settled.Owes)
	require.Equal(t, body, settled.Body)
	edited := read
	edited.PullRequest = found(41, forge.PullRequestOpen, "#### Description\n\nmy words\n\n###### Tested on\n\nevidence\n")
	settled = Settle(update, recorded, edited)
	require.Equal(t, []model.PublicationStep{model.StepEdit}, settled.Owes, "the description isn't what it sent")
	require.Equal(t, "#### Description\n\njq: update to 1.7\n\n###### Tested on\n\nevidence\n", settled.Body, "Tested on landed; the Description stays as recorded, for the merge to keep as theirs")
}

// A step still owed fails the command only where it failed or GitHub
// refused it; one awaiting an answer, unread, or that may have been sent
// is reported and doesn't (docs/designs/publication-record.md, `pending`).
func TestAPendingStepFailsTheCommandOnlyWhereItFailed(t *testing.T) {
	t.Parallel()
	for reason, fails := range map[PendingReason]bool{
		PendingFailed: true, PendingRefused: true,
		PendingAnswer: false, PendingMaySent: false, PendingUnread: false,
	} {
		require.Equal(t, fails, Pending{Step: model.StepReady, Reason: reason}.FailsCommand(), reason)
	}
}

// Settling keeps a publication that still owes a step, with what it owes,
// until a new submission replaces it: a plan alone deleted it, so a --plan
// lost an owed reviewer yes. Archiving or forgetting the branch ends it
// (#191, group A).
func TestSettlingKeepsAPublicationThatStillOwesAStep(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "notes"})
	require.NoError(t, err)
	at := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	recorded := &model.Publication{ID: "pub_1", Commit: "abc", At: at, Head: "ada/macports-ports:dockhand/notes", Base: "macports/macports-ports:master",
		Review: &model.ReviewAsk{Answer: model.ReviewYes, Reviewers: []model.Reviewer{{Login: "reviewer", Reviewed: at}}},
		Owes:   []model.PublicationStep{model.StepRerequest}}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.SetPublication(branch.ID, "", recorded) }))
	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	worktree, err := e.worktree(t.Context(), branch)
	require.NoError(t, err)

	settled, carried, _, err := e.settlePublication(t.Context(), worktree, branch, false)
	require.NoError(t, err)
	require.NotNil(t, carried)
	require.Equal(t, []model.PublicationStep{model.StepRerequest}, carried.Owes)
	require.NotNil(t, settled.Publication, "a plan keeps the publication that still owes the reviewers")
	require.True(t, settled.Publication.Same(*recorded))

	_, _, _, err = e.settlePublication(t.Context(), worktree, settled, true)
	require.NoError(t, err)
	ended, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.Nil(t, ended.Publication, "archiving or forgetting ends it")
}

// Settling reads the publication's recorded head in that head's own
// repository, not whichever fork is selected now: a push that landed in
// another fork is found there, and the publication owes nothing more
// (#191, group B).
func TestSettlingReadsTheRecordedHeadsRepository(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	commit := testsupport.Git(t, f.clone, "rev-parse", branch.Name)
	other := filepath.Join(filepath.Dir(f.upstream), "other.git")
	testsupport.Git(t, filepath.Dir(f.upstream), "clone", "-q", "--bare", f.upstream, other)
	testsupport.Git(t, f.clone, "remote", "add", "other", other)
	testsupport.Git(t, f.clone, "push", "-q", other, commit+":refs/heads/dockhand/notes")
	fake.Repos = map[string]string{"other/macports-ports": other}

	recorded := &model.Publication{ID: "pub_other", Commit: model.ObjectID(commit), At: time.Now().UTC(), Head: "other/macports-ports:dockhand/notes", Base: e.PullRequestRepository() + ":" + UpstreamBranch,
		Owes: []model.PublicationStep{model.StepPush}}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.SetPublication(branch.ID, "", recorded) }))
	branch, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	worktree, err := e.worktree(t.Context(), branch)
	require.NoError(t, err)
	settled, _, done, err := e.settlePublication(t.Context(), worktree, branch, false)
	require.NoError(t, err)
	require.Equal(t, []model.PublicationStep{model.StepPush}, done)
	require.Nil(t, settled.Publication, "the push landed in the head's own repository")
}
