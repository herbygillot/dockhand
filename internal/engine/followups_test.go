package engine

import (
	"errors"
	"fmt"
	"testing"

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
// publication-record.md, "Follow-ups are for the commit").
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
// group B; docs/designs/publication-record.md, "One read before the pull
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
// group B; docs/designs/publication-record.md, "Who continues what").
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
// (#191, group B; docs/designs/publication-record.md, "A create GitHub
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
// group C; docs/designs/publication-record.md, "The gate").
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
