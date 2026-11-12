package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/remotewrite"
	"github.com/herbygillot/dockhand/internal/store"
)

// The publication record's working half (docs/designs/publication-record.md):
// a submission records what it sets out to do before its first remote
// effect, and each step leaves what it owes in the transaction that writes
// its result. settle.go reads what an earlier one left.

// publication is the record a plan sets out with: its whole destination,
// the texts it writes, what the person asked, and the steps it owes, in
// order.
func (p SubmitPlan) publication(id, by string, served bool) model.Publication {
	record := model.Publication{ID: id, Commit: model.ObjectID(p.Commit), By: by, Served: served,
		Head: p.Head(), Base: p.Repository + ":" + UpstreamBranch, Expected: "",
		Asked: model.Asked{Draft: p.Request.Draft, NoCheck: p.Request.NoCheck, SkipNotification: p.Request.SkipNotification, Rewrite: p.Request.Rewrite, Types: p.Request.Types}}
	if p.RemoteHead.Exists {
		record.Expected = model.ObjectID(p.RemoteHead.Object)
	}
	if p.Earlier != nil {
		record.Earlier = p.Earlier.Ref.Number
	}
	if !p.Theirs {
		record.Title, record.Body, record.Written = p.Title, p.Body, p.Written
	}
	if p.ToDraft() {
		record.Owes = append(record.Owes, model.StepDraft)
	}
	if p.Pushes() {
		record.Owes = append(record.Owes, model.StepPush)
	}
	switch {
	case p.Existing == nil:
		record.Owes = append(record.Owes, model.StepOpen)
	default:
		record.Number = p.Existing.PullRequest.Ref.Number
		if !p.Theirs && (p.Title != p.Existing.PullRequest.Title || p.Body != p.Existing.PullRequest.Body) {
			record.Owes = append(record.Owes, model.StepEdit)
		}
	}
	if p.Request.Ready {
		record.Owes = append(record.Owes, model.StepReady)
	}
	if c := p.Carried; p.Request.Rerequest && c != nil && c.Review != nil && slices.Contains(c.Owes, model.StepRerequest) {
		// The marks come along, so no login is sent a second request.
		ask := *c.Review
		ask.Reviewers = slices.Clone(c.Review.Reviewers)
		record.Review = &ask
		record.Owes = append(record.Owes, model.StepRerequest)
	} else if logins := p.Rerequests(); p.Request.Rerequest && len(logins) > 0 {
		ask := &model.ReviewAsk{Answer: model.ReviewYes}
		reviewed := p.Branch.PullRequest.Observed.ReviewedAt
		for _, login := range logins {
			ask.Reviewers = append(ask.Reviewers, model.Reviewer{Login: login, Reviewed: reviewed})
		}
		record.Review = ask
		record.Owes = append(record.Owes, model.StepRerequest)
	}
	return record
}

// without is the publication with steps no longer owed.
func without(record model.Publication, steps ...model.PublicationStep) model.Publication {
	record.Owes = slices.DeleteFunc(slices.Clone(record.Owes), func(step model.PublicationStep) bool { return slices.Contains(steps, step) })
	return record
}

// publicationIs witnesses a write of a branch's publication: the branch's
// is the one given, by ID and what it owes, or none where want is nil.
func publicationIs(branch model.BranchID, want *model.Publication) func(store.Reader) bool {
	return func(r store.Reader) bool {
		got, err := r.Branch(branch)
		if err != nil {
			return false
		}
		if want == nil || got.Publication == nil {
			return want == nil && got.Publication == nil
		}
		return got.Publication.ID == want.ID && slices.Equal(got.Publication.Owes, want.Owes)
	}
}

// rewritePublication replaces a branch's publication in a transaction seen
// recorded, while the branch's is still was; fold writes what the step
// left into the same transaction, before it.
func (e *Engine) rewritePublication(ctx context.Context, branch model.BranchID, was string, next *model.Publication, fold func(store.Tx) error) error {
	err := store.Recorded(ctx, e.Store, e.Repository, func(tx store.Tx) error {
		if fold != nil {
			if err := fold(tx); err != nil {
				return err
			}
		}
		return tx.SetPublication(branch, was, next)
	}, publicationIs(branch, next))
	if err != nil {
		return fmt.Errorf("recording the submission's progress: %w", err)
	}
	return nil
}

// ServeLimitReached is serve's admission refused: today's rows count up to
// its limit, or the records took on the count by rows today, when the
// openings serve made earlier that day were counted only by events.
type ServeLimitReached struct {
	Limit    int
	Upgraded bool
}

func (e *ServeLimitReached) Error() string {
	if e.Upgraded {
		return "dockhand's records took on serve's count today, which counted the day's earlier pull requests only by events; serve opens none more until midnight"
	}
	return fmt.Sprintf("today's limit of %s has been reached (serve.submit_limit)", prose.Plural(e.Limit, "pull request"))
}

// recordPublication is a submission's first transaction, seen recorded
// before any remote effect: the publication, written only where the
// branch has none, and, for serve where it opens a pull request, the
// admission and its place, one transaction, so a second pass or process
// can't interleave (serve's count).
func (e *Engine) recordPublication(ctx context.Context, plan SubmitPlan, record *model.Publication) (*model.ServeOpening, error) {
	var opening *model.ServeOpening
	err := store.Recorded(ctx, e.Store, e.Repository, func(tx store.Tx) error {
		opening = nil
		if plan.served && slices.Contains(record.Owes, model.StepOpen) {
			now := cmp.Or(plan.admission.Now, e.now())
			day := dayStart(now)
			at, _, ok, err := tx.ServeUpgrade()
			if err != nil {
				return err
			}
			if ok && !dayStart(at.In(now.Location())).Before(day) {
				return &ServeLimitReached{Limit: plan.admission.Limit, Upgraded: true}
			}
			rows, err := tx.ServeOpenings()
			if err != nil {
				return err
			}
			if !ServeAdmits(rows, day, plan.admission.Limit) {
				return &ServeLimitReached{Limit: plan.admission.Limit}
			}
			place := model.ServeOpening{Base: record.Base, Head: record.Head, After: record.Earlier, Holding: true}
			if place.ID, err = tx.AddServeOpening(place); err != nil {
				return err
			}
			opening = &place
		}
		return tx.SetPublication(plan.Branch.ID, "", record)
	}, publicationIs(plan.Branch.ID, record))
	if err != nil {
		return nil, fmt.Errorf("recording the submission before it begins: %w; nothing was pushed", err)
	}
	return opening, nil
}

// markTried records serve's create as tried before it is sent, seen
// recorded: from then nothing dockhand reads gives the place up, since a
// read that finds no pull request doesn't show the create didn't land.
func (e *Engine) markTried(ctx context.Context, opening *model.ServeOpening) error {
	tried := *opening
	if tried.Tried.IsZero() {
		tried.Tried = e.now()
	}
	tried.Holding = false
	err := store.Recorded(ctx, e.Store, e.Repository, func(tx store.Tx) error {
		return tx.UpdateServeOpening(tried)
	}, func(r store.Reader) bool {
		rows, err := r.ServeOpenings()
		return err == nil && slices.ContainsFunc(rows, func(o model.ServeOpening) bool { return o.ID == tried.ID && !o.Tried.IsZero() })
	})
	if err != nil {
		return fmt.Errorf("marking serve's create before sending it: %w; nothing was opened", err)
	}
	*opening = tried
	return nil
}

// answeredCreate records what serve's create's own answer leaves of its
// row: a refusal releases it where nothing of it is unanswered; one that
// says a pull request exists for the head leaves it for the read; no
// answer marks it unanswered. A write that fails leaves the row as it is,
// which counts it, the side that errs toward opening fewer.
func (e *Engine) answeredCreate(ctx context.Context, opening *model.ServeOpening, err error) {
	if opening == nil || errors.Is(err, forge.ErrPullRequestExists) {
		return
	}
	if errors.Is(err, forge.ErrRejected) {
		if opening.Refused() {
			_ = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error { return tx.ReleaseServeOpening(opening.ID) })
		}
		return
	}
	unanswered := *opening
	unanswered.Unanswered = true
	if e.Store.Update(ctx, e.Repository, func(tx store.Tx) error { return tx.UpdateServeOpening(unanswered) }) == nil {
		*opening = unanswered
	}
}

// provenanceOf is how the record knows the pull request numbered number,
// empty where it doesn't, or knows another.
func provenanceOf(pr *model.PullRequest, number int) model.Provenance {
	if pr == nil || pr.Number != number {
		return ""
	}
	return pr.Provenance
}

// endPublication ends a publication that owes nothing more, seen recorded.
func (e *Engine) endPublication(ctx context.Context, branch model.BranchID, record model.Publication) error {
	if len(record.Owes) > 0 {
		return nil
	}
	return e.rewritePublication(ctx, branch, record.ID, nil, nil)
}

// rerequest asks the publication's reviewers to review again, login by
// login, and records each mark: the pull request's pending requests and
// each login's own latest review are read first (ReviewerStep); a login
// with no mark is marked "may have been sent" in a transaction seen
// recorded, then asked, and its answer leaves the mark (ReviewerAnswered).
// No login is sent two requests by one publication. The receipt goes onto
// the pull request's record when the publication ends.
func (e *Engine) rerequest(ctx context.Context, branch model.BranchID, record *model.Publication) ([]string, error) {
	current, err := e.Branch(ctx, branch)
	if err != nil {
		return nil, err
	}
	pr := current.PullRequest
	if pr == nil {
		return nil, fmt.Errorf("%s has no pull request recorded; asking the reviewers again is still owed", current.ShortName())
	}
	ref := pullRequestRef(pr.Repository, pr.Number)
	// Follow-ups are for the commit: the fork's branch is read, through
	// the pull request, just before the call.
	status, err := e.forge().Inspect(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("reading #%d before asking its reviewers again: %w; asking them is still owed, and dockhand submit %s finishes it", pr.Number, err, current.ShortName())
	}
	ask := *record.Review
	ask.Reviewers = slices.Clone(ask.Reviewers)
	var asked, send []string
	var why []string
	for i, reviewer := range ask.Reviewers {
		switch ReviewerStep(reviewer, status) {
		case ReviewerUnread:
			return nil, fmt.Errorf("#%d's pending review requests couldn't be read whole; asking the reviewers again is still owed, and dockhand submit %s finishes it", pr.Number, current.ShortName())
		case ReviewerSend:
			ask.Reviewers[i].Sent = model.MaySent
			send = append(send, reviewer.Login)
		case ReviewerPending:
			asked = append(asked, reviewer.Login)
		}
	}
	if len(send) > 0 {
		marked := *record
		marked.Review = &ask
		if err := e.rewritePublication(ctx, branch, record.ID, &marked, nil); err != nil {
			return nil, err
		}
		*record = marked
		err := remotewrite.Ask(ctx, e.pullRequests(), reviewBasis(pr, record), ref, send)
		sent, words := ReviewerAnswered(err)
		for i, reviewer := range ask.Reviewers {
			if slices.Contains(send, reviewer.Login) {
				ask.Reviewers[i].Sent, ask.Reviewers[i].Why = sent, words
			}
		}
		switch sent {
		case model.SentRequest:
			asked = append(asked, send...)
		case model.RefusedToAsk:
			why = append(why, fmt.Sprintf("GitHub refused to ask %s: %s", strings.Join(send, ", "), words))
		case model.MaySent:
			why = append(why, fmt.Sprintf("asking %s to review again got no answer (%v), so it may have been sent, and isn't sent again; ask on GitHub if it wasn't", strings.Join(send, ", "), err))
		case model.NotSent:
			why = append(why, fmt.Sprintf("asking %s: %v", strings.Join(send, ", "), err))
		}
	}
	done := *record
	done.Review = &ask
	if !slices.ContainsFunc(ask.Reviewers, func(r model.Reviewer) bool { return r.Sent == model.NotSent && !slices.Contains(asked, r.Login) }) {
		done = without(done, model.StepRerequest)
	}
	receipt := &model.ReviewReceipt{Head: record.Commit, Reviewers: ask.Reviewers}
	err = e.rewritePublication(ctx, branch, record.ID, &done, func(tx store.Tx) error {
		b, err := tx.Branch(branch)
		if err != nil || b.PullRequest == nil {
			return err
		}
		b.PullRequest.Rerequested = receipt
		if err := tx.UpdateBranch(b); err != nil {
			return err
		}
		if len(asked) == 0 {
			return nil
		}
		_, err = tx.AppendEvent(model.Event{At: e.now(), Branch: branch, Kind: model.EventBranchRerequest, Level: model.LevelInfo,
			Message: fmt.Sprintf("asked %s to review #%d again", "@"+strings.Join(asked, ", @"), pr.Number)})
		return err
	})
	*record = done
	if err != nil {
		return asked, err
	}
	if len(why) > 0 {
		return asked, errors.New(strings.Join(why, "; "))
	}
	return asked, nil
}

// reviewBasis is the basis a reviewer request states to the gate: Made on
// a pull request dockhand opened, or on an old record that isn't adopted,
// as today; otherwise Named, since the person answered yes to this exact
// request (docs/designs/publication-record.md, "The gate").
func reviewBasis(pr *model.PullRequest, record *model.Publication) remotewrite.Basis {
	if !pr.Adopted && pr.Provenance != model.Found {
		return remotewrite.Made
	}
	if record.Review != nil && record.Review.Answer == model.ReviewYes {
		return remotewrite.Named
	}
	return remotewrite.Invited
}

// endRefused ends a publication whose first remaining step was refused
// with nothing done, as a draft GitHub refused or a push the fork
// refused: there is nothing to settle, and the next submit is a new
// attempt. serve's place, never tried, goes with it.
func (e *Engine) endRefused(ctx context.Context, branch model.BranchID, record model.Publication) error {
	return e.rewritePublication(ctx, branch, record.ID, nil, func(tx store.Tx) error {
		if !record.Served {
			return nil
		}
		rows, err := tx.ServeOpenings()
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.Head == record.Head && row.Base == record.Base && row.Tried.IsZero() && row.Opened.IsZero() {
				return tx.ReleaseServeOpening(row.ID)
			}
		}
		return nil
	})
}

// pendingAfter are the steps a submission that returned with err still
// owes, read from its record, each with why: a reviewer marked as maybe
// sent, or refused, is said so; any other step failed with err.
func (e *Engine) pendingAfter(ctx context.Context, branch model.BranchID, err error) []Pending {
	current, readErr := e.Branch(ctx, branch)
	if readErr != nil || current.Publication == nil {
		return nil
	}
	record := current.Publication
	var pending []Pending
	for _, step := range record.Owes {
		pending = append(pending, Pending{Step: step, Reason: PendingFailed, Detail: err.Error()})
	}
	if record.Review != nil {
		for _, reviewer := range record.Review.Reviewers {
			switch reviewer.Sent {
			case model.MaySent:
				pending = append(pending, Pending{Step: model.StepRerequest, Reason: PendingMaySent, Detail: reviewer.Login})
			case model.RefusedToAsk:
				pending = append(pending, Pending{Step: model.StepRerequest, Reason: PendingRefused, Detail: reviewer.Login + ": " + reviewer.Why})
			}
		}
	}
	return pending
}
