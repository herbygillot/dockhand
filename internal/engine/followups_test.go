package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/remotewrite"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// A follow-up reads the pull request's head itself, just before its call:
// where someone moved the fork's branch after the push, marking ready was
// for the commit submitted, so it isn't made, the publication ends, and
// what it owed comes back as dropped (#191, group B; docs/designs/
// publication-record.md at 0e0ad767, "Follow-ups are for the commit").
func TestAFollowUpIsForTheCommitSubmitted(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Draft: true})
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	number := submitted.PullRequest.Ref.Number
	require.True(t, fake.Draft(number))

	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Ready: true})
	require.NoError(t, err)
	base := testsupport.Git(t, f.clone, "rev-parse", "origin/master")
	fake.Observing = func(ref forge.PullRequestRef) {
		if ref.Number == number {
			testsupport.Git(t, fake.Fork, "update-ref", "refs/heads/"+plan.RemoteBranch(), base)
		}
	}
	submitted, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	require.Equal(t, model.ObjectID(base), submitted.MovedTo)
	require.Equal(t, []model.PublicationStep{model.StepReady}, submitted.Dropped)
	require.False(t, submitted.Ready)
	require.Empty(t, fake.Readied, "ready was for the commit submitted")
	require.True(t, fake.Draft(number))
	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.Nil(t, branch.Publication, "the head at another commit ends the publication")
}

// The title and description are read once more just before the edit: one
// changed on GitHub after submit's read before the push isn't written
// over, the edit stays owed, and the error names what changed (#191,
// group B; docs/designs/publication-record.md at 0e0ad767, "One read before the pull
// request is written").
func TestTheEditReadsTheTextJustBeforeIt(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	number := submitted.PullRequest.Ref.Number

	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Title: "jq: update to 1.8.1, retitled"})
	require.NoError(t, err)
	reads := 0
	fake.Observing = func(ref forge.PullRequestRef) {
		if ref.Number != number {
			return
		}
		// The first read is submit's before the push; a person edits the
		// title on GitHub after it.
		if reads++; reads == 2 {
			fake.PRs[number].Title = "jq: a person's title"
		}
	}
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.ErrorContains(t, err, "title changed on GitHub during the submission, so it wasn't updated")
	require.Equal(t, "jq: a person's title", fake.PRs[number].Title, "the person's title isn't written over")
	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.NotNil(t, branch.Publication)
	require.Contains(t, branch.Publication.Owes, model.StepEdit)
}

// What an earlier submission asked carries only to one for the same
// account and destination: its commit and head, its base, and the pull
// request it was for. A ready still owed carries under the same login and
// destination, and not under another login, or for another base (#191,
// group B; docs/designs/publication-record.md at 0e0ad767, "Who continues what").
func TestWhatIsCarriedChecksTheAccountAndDestination(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Draft: true})
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	number := submitted.PullRequest.Ref.Number
	require.True(t, fake.Draft(number))

	carries := func(by, base string) bool {
		t.Helper()
		earlier := plan.publication(store.NewID("pub"), by, false)
		earlier.Number, earlier.Owes = number, []model.PublicationStep{model.StepReady}
		if base != "" {
			earlier.Base = base
		}
		require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
			current, err := tx.Branch(branch.ID)
			if err != nil {
				return err
			}
			was := ""
			if current.Publication != nil {
				was = current.Publication.ID
			}
			return tx.SetPublication(branch.ID, was, &earlier)
		}))
		current, err := e.Branch(t.Context(), branch.ID)
		require.NoError(t, err)
		next, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: current, NoCheck: true})
		require.NoError(t, err)
		require.Equal(t, next.Carried != nil, next.Request.Ready)
		return next.Carried != nil
	}
	require.True(t, carries(plan.Login.Account, ""), "the same login and destination")
	require.False(t, carries("someone-else", ""), "another login")
	require.False(t, carries(plan.Login.Account, "someone/macports-ports:master"), "another base")
}

// A create GitHub refuses is its answer: nothing was opened, so the
// publication ends with the refusal said rather than owing open forever
// (#191, group B; docs/designs/publication-record.md at 0e0ad767, "A create GitHub
// refuses").
func TestACreateGitHubRefusesEndsThePublication(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	fake.Refuse = map[string]string{"Create": "Validation Failed"}
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.ErrorContains(t, err, "GitHub refused to open the pull request")
	require.ErrorContains(t, err, "Validation Failed")
	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.Nil(t, branch.Publication, "nothing is owed after a refusal")
}

// Returning a pull request to draft ends the publication only where GitHub
// refused it; an error that may have done it leaves the step owed, for
// settling to read (#191, group B).
func TestOnlyARefusedDraftEndsThePublication(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err  error
		ends bool
	}{
		"refused":   {fmt.Errorf("%w: Resource not accessible by integration", forge.ErrRejected), true},
		"no answer": {errors.New("github: connection reset"), false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			e, _ := f.withPreparer(t)
			fake := f.withFork(t, e)
			branch := committedUpdate(t, e)
			plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
			require.NoError(t, err)
			_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
			require.NoError(t, err)

			branch, err = e.Branch(t.Context(), branch.ID)
			require.NoError(t, err)
			plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Draft: true})
			require.NoError(t, err)
			fake.DraftRefused = tc.err
			_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
			require.Error(t, err)
			branch, err = e.Branch(t.Context(), branch.ID)
			require.NoError(t, err)
			if tc.ends {
				require.Nil(t, branch.Publication)
				return
			}
			require.NotNil(t, branch.Publication)
			require.Contains(t, branch.Publication.Owes, model.StepDraft)
		})
	}
}

// A found pull request is stated to the gate as named, never as made:
// Made only where the record says dockhand opened it, or an old record
// with no provenance that isn't adopted keeps the right it had (#191,
// group C; docs/designs/publication-record.md at 0e0ad767, "The gate").
func TestAFoundPullRequestIsStatedAsNamed(t *testing.T) {
	t.Parallel()
	existing := &forge.PullRequestObservation{Found: true, PullRequest: forge.PullRequest{Ref: forge.PullRequestRef{Number: 7}}}
	for provenance, want := range map[model.Provenance]remotewrite.Basis{model.Opened: remotewrite.Made, "": remotewrite.Made, model.Found: remotewrite.Named} {
		plan := SubmitPlan{Existing: existing, Branch: model.Branch{PullRequest: &model.PullRequest{Number: 7, Provenance: provenance}}}
		require.Equal(t, want, plan.editBasis(), "provenance %q", provenance)
	}
	require.Equal(t, remotewrite.Named, SubmitPlan{Existing: existing}.editBasis(), "one found with no record")
	require.Equal(t, remotewrite.Named, SubmitPlan{Existing: existing, Branch: model.Branch{PullRequest: &model.PullRequest{Number: 6, Provenance: model.Opened}}}.editBasis(), "another than the one recorded")
	basis, err := (&Engine{}).pullRequestBasis(t.Context(), &model.PullRequest{Number: 7, Provenance: model.Found}, "ada")
	require.NoError(t, err)
	require.Equal(t, remotewrite.Named, basis)
}

// An old record with no provenance becomes found at its next read where
// GitHub names another author than the account dockhand acts as, and
// keeps its rights where the author is the person (#191, group C).
func TestAnOldRecordBecomesFoundWhereAnotherIsItsAuthor(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	number := submitted.PullRequest.Ref.Number
	old := func() {
		require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error {
			current, err := tx.Branch(branch.ID)
			if err != nil {
				return err
			}
			pr := *current.PullRequest
			pr.Provenance = ""
			current.PullRequest = &pr
			return tx.UpdateBranch(current)
		}))
	}
	provenance := func() model.Provenance {
		current, err := e.Branch(t.Context(), branch.ID)
		require.NoError(t, err)
		return current.PullRequest.Provenance
	}

	old()
	fake.PRs[number].Author = plan.Login.Account
	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.Equal(t, model.Provenance(""), provenance(), "the person's own keeps the right it had")

	fake.PRs[number].Author = "someone-else"
	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	require.Equal(t, model.Found, provenance(), "another author makes it found")

	old()
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	next, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: current, NoCheck: true, Title: "jq: update to 1.8.1, retitled"})
	require.NoError(t, err)
	require.Equal(t, remotewrite.Named, next.editBasis(), "the plan's own read withdraws Made")
	_, err = e.ApplySubmit(t.Context(), next, model.AnsweredAsked)
	require.NoError(t, err)
	require.Equal(t, model.Found, provenance(), "and the submission records it")
}

// Reviewers, one by one: each login's answer is for its own latest review,
// read with the plan; each is asked in a call of its own, so GitHub
// refusing one leaves the other asked, with GitHub's words kept; and one
// who reviewed again after the answer isn't asked, and is said (#191,
// group D).
func TestEachReviewerIsAskedForTheirOwnReview(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	g := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	first, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	number := first.PullRequest.Ref.Number
	base := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	g.Status = forge.PullRequestStatus{Review: "changes-requested", ChangesRequested: 3, ChangesRequestedBy: []string{"ana", "bo", "cy"}, ReviewedAt: base.Add(3 * time.Hour),
		LatestReview: map[string]time.Time{"ana": base.Add(time.Hour), "bo": base.Add(2 * time.Hour), "cy": base.Add(3 * time.Hour)}}
	e.options.Now = func() time.Time { return base.Add(4 * time.Hour) }
	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	testsupport.WriteFiles(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# review addressed\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-qam", "jq: address review")
	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Rerequest: true})
	require.NoError(t, err)
	record := plan.publication("pub_x", "", false)
	reviewed := map[string]time.Time{}
	for _, r := range record.Review.Reviewers {
		reviewed[r.Login] = r.Reviewed
	}
	require.Equal(t, map[string]time.Time{"ana": base.Add(time.Hour), "bo": base.Add(2 * time.Hour), "cy": base.Add(3 * time.Hour)}, reviewed, "each login's own latest review")

	// GitHub won't ask bo; cy reviews again after the answer.
	g.Unrequestable = map[string]string{"bo": "not a collaborator"}
	status := g.Status
	status.LatestReview = map[string]time.Time{"ana": base.Add(time.Hour), "bo": base.Add(2 * time.Hour), "cy": base.Add(5 * time.Hour)}
	g.Status = status
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.ErrorContains(t, err, "GitHub refused to ask bo")
	require.Equal(t, []string{"ana"}, submitted.Asked, "ana is asked though GitHub refused bo")
	require.Equal(t, []string{"cy"}, submitted.ReviewedSince)
	require.Equal(t, []string{"ana"}, g.RerequestedOn[number])
	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	marks := map[string]model.Reviewer{}
	for _, r := range branch.PullRequest.Rerequested.Reviewers {
		marks[r.Login] = r
	}
	require.Equal(t, model.SentRequest, marks["ana"].Sent)
	require.Equal(t, model.RefusedToAsk, marks["bo"].Sent)
	require.Contains(t, marks["bo"].Why, "not a collaborator", "the receipt keeps GitHub's words")
	require.Equal(t, model.ReviewedAgain, marks["cy"].Sent)
	require.Nil(t, branch.Publication, "every reviewer has an outcome")
}

// The reviewer step goes by its own reading of the head: where the pull
// request moved on after the follow-up's check, nobody is asked or marked,
// and asking stays owed for the commit submitted (#191, group B; Codex's
// review of c76b6fc0).
func TestTheReviewerStepGoesByItsOwnReadingOfTheHead(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	g := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	first, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	number := first.PullRequest.Ref.Number
	base := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	g.Status = forge.PullRequestStatus{Review: "changes-requested", ChangesRequested: 1, ChangesRequestedBy: []string{"ana"}, ReviewedAt: base, LatestReview: map[string]time.Time{"ana": base}}
	e.options.Now = func() time.Time { return base.Add(time.Hour) }
	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	testsupport.WriteFiles(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# review addressed\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-qam", "jq: address review")
	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Rerequest: true})
	require.NoError(t, err)
	moved := testsupport.Git(t, f.clone, "rev-parse", "origin/master")
	g.Inspecting = func(ref forge.PullRequestRef) {
		if ref.Number == number {
			testsupport.Git(t, g.Fork, "update-ref", "refs/heads/"+plan.RemoteBranch(), moved)
		}
	}
	require.Equal(t, []string{"ana"}, plan.Rerequests())
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.ErrorContains(t, err, "so its reviewers weren't asked again")
	require.Empty(t, submitted.Asked)
	require.Empty(t, g.RerequestedOn[number], "nobody is asked")
	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.NotNil(t, branch.Publication)
	require.Contains(t, branch.Publication.Owes, model.StepRerequest)
	require.Equal(t, model.NotSent, branch.Publication.Review.Reviewers[0].Sent, "nobody is marked")
}

// A draft change GitHub answered on a pull request the branch had no
// record of yet is kept for settling: the push failing after it, the
// record settling makes says the pull request is a draft, as GitHub has
// it (#191, group B; Codex's review).
func TestADraftChangeOnAFoundPullRequestIsFolded(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	_, err = fake.Create(t.Context(), forge.PullRequestInput{Repository: plan.Repository, BaseBranch: UpstreamBranch, HeadRepository: plan.HeadRepository, HeadBranch: plan.RemoteBranch(),
		Desired: forge.PullRequestContent{Title: "jq: update to 1.8.1", Body: "a person's"}})
	require.NoError(t, err)
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Draft: true})
	require.NoError(t, err)
	require.True(t, plan.ToDraft())
	require.Nil(t, plan.Branch.PullRequest, "no record of it yet")

	// The fork refuses the push after the draft change landed.
	require.NoError(t, os.Chmod(filepath.Join(fake.Fork, "objects"), 0o555))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(fake.Fork, "objects"), 0o755) })
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.Error(t, err)
	require.True(t, submitted.Drafted)
	require.True(t, fake.Draft(plan.Existing.PullRequest.Ref.Number))

	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.NotNil(t, current.Publication)
	worktree, err := e.worktree(t.Context(), current)
	require.NoError(t, err)
	_, _, _, err = e.settlePublication(t.Context(), worktree, current, false)
	require.NoError(t, err)
	current, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.NotNil(t, current.PullRequest)
	require.True(t, current.PullRequest.Draft, "GitHub has it as a draft")
}

// What the person gave with the plan is written in the submission's first
// transaction, before any remote effect: the title and the note stand
// though the push then fails (#191, group E; docs/designs/
// publication-record.md at 0e0ad767, "The record").
func TestTheFirstTransactionWritesWhatThePersonGave(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	note := "Upstream moved its tarballs."
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Title: "jq: update to 1.8.1, upstream moved", Note: &note})
	require.NoError(t, err)
	require.NoError(t, os.Chmod(filepath.Join(fake.Fork, "objects"), 0o555))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(fake.Fork, "objects"), 0o755) })
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.Error(t, err, "the fork refuses the push")
	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.Equal(t, "jq: update to 1.8.1, upstream moved", branch.Title)
	require.Equal(t, note, branch.Note)
}

// The question about the reviewers is recorded however it was answered: a
// no is kept and owes nothing, and a question nobody was there to answer
// stays owed, nobody asked, until a run a person attends answers it
// (#191, group E).
func TestTheReviewerQuestionIsRecordedHoweverItWasAnswered(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	g := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	base := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	g.Status = forge.PullRequestStatus{Review: "changes-requested", ChangesRequested: 1, ChangesRequestedBy: []string{"ana"}, ReviewedAt: base, LatestReview: map[string]time.Time{"ana": base}}
	e.options.Now = func() time.Time { return base.Add(time.Hour) }
	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	commit := func(line string) model.Branch {
		testsupport.WriteFiles(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n" + line + "\n"})
		testsupport.Git(t, branch.Worktree, "commit", "-qam", "jq: "+line)
		current, err := e.Branch(t.Context(), branch.ID)
		require.NoError(t, err)
		return current
	}

	// No: recorded, nothing owed.
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: commit("# a no"), NoCheck: true, RerequestAnswer: model.ReviewNo})
	require.NoError(t, err)
	require.Equal(t, model.ReviewNo, plan.publication("pub_no", "", false).Review.Answer)
	require.NotContains(t, plan.publication("pub_no", "", false).Owes, model.StepRerequest)

	// Nobody there: owed, nobody asked; the next attended yes asks.
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: commit("# nobody"), NoCheck: true})
	require.NoError(t, err)
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	require.Empty(t, submitted.Asked)
	require.Empty(t, g.Rerequested)
	require.Equal(t, []Pending{{Step: model.StepRerequest, Reason: PendingAnswer, Detail: "nobody was there to answer whether to ask the reviewers again"}}, submitted.Pending, "said though the submission finished")
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.NotNil(t, current.Publication, "the question stays owed")
	require.Equal(t, model.ReviewNotAsked, current.Publication.Review.Answer)
	require.Contains(t, current.Publication.Owes, model.StepRerequest)

	again, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: current, NoCheck: true})
	require.NoError(t, err)
	require.Equal(t, []string{"ana"}, again.Rerequests(), "the next run puts the question")
	again.Request.Rerequest, again.Request.RerequestAnswer = true, model.ReviewYes
	submitted, err = e.ApplySubmit(t.Context(), again, model.AnsweredAsked)
	require.NoError(t, err)
	require.Equal(t, []string{"ana"}, submitted.Asked)
	current, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	require.Nil(t, current.Publication)
}

// An unread review is no answer: where the plan couldn't read each
// reviewer's latest review, the pull request's verdict time doesn't stand
// in for theirs, so a reviewer whose own newer, comment-only review the
// reviewer step reads is still asked (#191; Codex's review of group D).
func TestAReviewThePlanCouldntReadDropsNobody(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	g := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	base := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	g.Status = forge.PullRequestStatus{Review: "changes-requested", ChangesRequested: 1, ChangesRequestedBy: []string{"ana"}, ReviewedAt: base, LatestReview: map[string]time.Time{"ana": base.Add(2 * time.Hour)}}
	e.options.Now = func() time.Time { return base.Add(3 * time.Hour) }
	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	testsupport.WriteFiles(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# review addressed\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-qam", "jq: address review")
	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	g.InspectErr = errors.New("github: 502")
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Rerequest: true})
	require.NoError(t, err)
	require.True(t, plan.ReviewsUnread)
	g.InspectErr = nil
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	require.Equal(t, []string{"ana"}, submitted.Asked, "the reviewer step's own reading decides")
	require.Empty(t, submitted.ReviewedSince)
}

// A plan that continues an earlier submission of its commit says what that
// one did, and, until the request carries them (#195), the flags it was
// given that this run leaves out: a --draft submission whose opening
// failed, continued plain, would open ready, unsaid (#191, group F).
func TestAContinuationSaysWhatTheEarlierSubmitDidAndWasGiven(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Draft: true, SkipNotification: true})
	require.NoError(t, err)
	testsupport.Git(t, f.clone, "push", "-q", fake.Fork, plan.Commit+":refs/heads/"+plan.RemoteBranch())
	earlier := plan.publication(store.NewID("pub"), plan.Login.Account, false)
	earlier.Owes = []model.PublicationStep{model.StepPush, model.StepOpen}
	require.NoError(t, e.Store.Update(t.Context(), e.Repository, func(tx store.Tx) error { return tx.SetPublication(branch.ID, "", &earlier) }))

	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	next, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: current, NoCheck: true})
	require.NoError(t, err)
	said := next.Continues()
	require.NotNil(t, said)
	require.Equal(t, []string{"pushed " + model.ObjectID(plan.Commit).Short()}, said.Did)
	require.Equal(t, []string{"--draft", "--skip-notification"}, said.NotCarried)

	again, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: current, NoCheck: true, Draft: true, SkipNotification: true})
	require.NoError(t, err)
	require.Empty(t, again.Continues().NotCarried, "given again, nothing is left out")
}

// What a submission still owes is said on every return, each step with its
// own reason: an opening that got no answer failed, and the ready after it
// waits on it, not failed with its error (#191, group F).
func TestEachPendingStepHasItsOwnReason(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	fake := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, Draft: true, Ready: false})
	require.NoError(t, err)
	plan.Request.Draft, plan.Request.Ready = false, true
	fake.CreateFails = true
	submitted, err := e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.Error(t, err)
	reasons := map[model.PublicationStep]PendingReason{}
	for _, owed := range submitted.Pending {
		reasons[owed.Step] = owed.Reason
	}
	require.Equal(t, PendingFailed, reasons[model.StepOpen])
	require.Equal(t, PendingWaits, reasons[model.StepReady], "not reached")
	for _, owed := range submitted.Pending {
		if owed.Step == model.StepReady {
			require.Equal(t, "waits on opening the pull request", owed.Detail)
			require.False(t, owed.FailsCommand())
		}
	}
}

// A --no-check submission names the branch's last check that passed on an
// earlier commit, and the files changed since, while saying the files
// submitted weren't checked (#246).
func TestANoCheckSubmissionNamesTheEarlierPass(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	f.withFork(t, e)
	branch := committedUpdate(t, e)
	checked(t, e, branch, model.OutcomePassed, model.OutcomePassed)
	testsupport.WriteFiles(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\ndepends_lib port:oniguruma6\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-qam", "jq: depend on oniguruma6")
	branch, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Contains(t, plan.Body, "no Dockhand check result is included")
	require.Regexp(t, `An earlier commit was checked: check-\d+ passed for [0-9a-f]{7}; since then textproc/jq/Portfile changed, which no check here covers\. The files submitted here were not checked\.`, plan.Body)
}

// A reviewer no the person gave for a commit carries across a push that
// failed: the retry of that commit doesn't ask again, and its record keeps
// the no, owing nothing for the reviewers (#191; Codex's review of group
// E, 0644188b).
func TestReview090NoAnswerCarriesAcrossFailedPush(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	g := f.withFork(t, e)
	branch := committedUpdate(t, e)
	plan, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.NoError(t, err)
	base := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	g.Status = forge.PullRequestStatus{Review: "changes-requested", ChangesRequested: 1, ChangesRequestedBy: []string{"ana"}, ReviewedAt: base, LatestReview: map[string]time.Time{"ana": base}}
	e.options.Now = func() time.Time { return base.Add(time.Hour) }
	_, err = e.RefreshPullRequests(t.Context())
	require.NoError(t, err)
	testsupport.WriteFiles(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# review addressed\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-qam", "jq: address review")
	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	plan, err = e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true, RerequestAnswer: model.ReviewNo})
	require.NoError(t, err)
	require.Equal(t, []string{"ana"}, plan.Rerequests())
	require.NoError(t, os.Chmod(filepath.Join(g.Fork, "objects"), 0o555))
	_, err = e.ApplySubmit(t.Context(), plan, model.AnsweredAsked)
	require.Error(t, err, "the fork refuses the push")
	require.NoError(t, os.Chmod(filepath.Join(g.Fork, "objects"), 0o755))

	branch, err = e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	again, err := e.PlanSubmit(t.Context(), SubmitRequest{Branch: branch, NoCheck: true})
	require.NoError(t, err)
	require.Empty(t, again.Rerequests(), "the no isn't asked again")
	record := again.publication("pub_again", "", false)
	require.NotNil(t, record.Review)
	require.Equal(t, model.ReviewNo, record.Review.Answer)
	require.NotContains(t, record.Owes, model.StepRerequest)
}
