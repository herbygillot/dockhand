package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/failpoint"
	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/remotewrite"
	"github.com/herbygillot/dockhand/internal/store"
)

// The publication record's working half (docs/designs/publication-record.md at 0e0ad767):
// a submission records what it sets out to do before its first remote
// effect, and each step leaves what it owes in the transaction that writes
// its result. settle.go reads what an earlier one left.

// publication is the record a plan sets out with: its whole destination,
// the texts it writes, what the person asked, and the steps it owes, in
// order.
func (p SubmitPlan) publication(id, by string, served bool) model.Publication {
	record := model.Publication{ID: id, Commit: model.ObjectID(p.Commit), By: by, Served: served,
		Head: p.Head(), Base: p.Repository + ":" + UpstreamBranch, Expected: "",
		Asked: model.Asked{Draft: p.Request.Draft, NoCheck: p.Request.NoCheck, SkipNotification: p.Request.SkipNotification, Rewrite: p.Request.Rewrite, Types: p.Request.Types}, RequestID: p.Request.RequestID}
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
	// The question about the reviewers is recorded however it was
	// answered: a yes is acted on, a no is kept and not asked again, and a
	// question nobody answered stays owed for the next run a person
	// attends (#191, group E).
	answer := p.Request.RerequestAnswer
	if p.Request.Rerequest {
		answer = model.ReviewYes
	}
	if c := p.Carried; c != nil && c.Review != nil && slices.Contains(c.Owes, model.StepRerequest) {
		// The marks come along, so no login is sent a second request.
		ask := *c.Review
		ask.Reviewers = slices.Clone(c.Review.Reviewers)
		ask.Answer = answer
		record.Review = &ask
		if answer != model.ReviewNo {
			record.Owes = append(record.Owes, model.StepRerequest)
		}
	} else if c != nil && c.Review != nil && c.Review.Answer == model.ReviewNo {
		// The no carries, owing nothing, as the person gave it for this
		// commit.
		ask := *c.Review
		ask.Reviewers = slices.Clone(c.Review.Reviewers)
		record.Review = &ask
	} else if logins := p.Rerequests(); len(logins) > 0 {
		ask := &model.ReviewAsk{Answer: answer}
		// Each login's answer is for its own latest review, as the plan
		// read it. An unread review is no answer's: the time stays empty,
		// never the pull request's one verdict time, and the reviewer
		// step's own reading decides (#191, Codex's review of group D).
		for _, login := range logins {
			ask.Reviewers = append(ask.Reviewers, model.Reviewer{Login: login, Reviewed: p.reviewedBy[login]})
		}
		record.Review = ask
		if answer != model.ReviewNo {
			record.Owes = append(record.Owes, model.StepRerequest)
		}
	}
	return record
}

// without is the publication with steps no longer owed.
func without(record model.Publication, steps ...model.PublicationStep) model.Publication {
	record.Owes = slices.DeleteFunc(slices.Clone(record.Owes), func(step model.PublicationStep) bool { return slices.Contains(steps, step) })
	return record
}

// publicationIs witnesses a write of a branch's publication: the branch's
// is the one given, the whole record with its reviewers' marks
// (model.Publication.Same), or none where want is nil. It compared the ID
// and the steps owed, so a mark that didn't commit passed (#191, group A).
func publicationIs(branch model.BranchID, want *model.Publication) func(store.Reader) bool {
	return func(r store.Reader) bool {
		got, err := r.Branch(branch)
		if err != nil {
			return false
		}
		if want == nil || got.Publication == nil {
			return want == nil && got.Publication == nil
		}
		return got.Publication.Same(*want)
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
		if err := e.recordAsked(tx, plan); err != nil {
			return err
		}
		if err := takeRequest(tx, plan.Branch.ID, record); err != nil {
			return err
		}
		if plan.served && slices.Contains(record.Owes, model.StepOpen) {
			now := cmp.Or(plan.admission.Now, e.now())
			day := dayStart(now)
			rows, err := tx.ServeOpenings()
			if err != nil {
				return err
			}
			// Every try of a row shares its place: a retry of a create that
			// got no answer, or of a place held, takes the row it holds, and
			// goes only where that row is among the first that count today,
			// oldest first (serve's count).
			if i := slices.IndexFunc(rows, func(o model.ServeOpening) bool {
				return o.Base == record.Base && o.Head == record.Head && o.Opened.IsZero() && (o.Holding || !o.Tried.IsZero())
			}); i >= 0 {
				if !ServeMaySend(rows, i, day, plan.admission.Limit) {
					return &ServeLimitReached{Limit: plan.admission.Limit}
				}
				held := rows[i]
				opening = &held
				return tx.SetPublication(plan.Branch.ID, replacedPublication(plan), record)
			}
			at, _, ok, err := tx.ServeUpgrade()
			if err != nil {
				return err
			}
			if ok && !dayStart(at.In(now.Location())).Before(day) {
				return &ServeLimitReached{Limit: plan.admission.Limit, Upgraded: true}
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
		return tx.SetPublication(plan.Branch.ID, replacedPublication(plan), record)
	}, publicationIs(plan.Branch.ID, record))
	if err != nil {
		return nil, fmt.Errorf("recording the submission before it begins: %w; nothing was pushed", err)
	}
	return opening, nil
}

// replacedPublication is the publication a submission's record replaces: the settled
// one its plan carried, where one was kept (settlePublication), and only
// that one.
func replacedPublication(plan SubmitPlan) string {
	if plan.Branch.Publication != nil {
		return plan.Branch.Publication.ID
	}
	return ""
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

// endPublication ends a publication that owes nothing more, seen
// recorded, and the request it took with it, where that is still the
// branch's (endRequest).
func (e *Engine) endPublication(ctx context.Context, branch model.BranchID, record model.Publication) error {
	if len(record.Owes) > 0 {
		return nil
	}
	if err := e.rewritePublication(ctx, branch, record.ID, nil, func(tx store.Tx) error { return endRequest(tx, branch, record) }); err != nil {
		return err
	}
	// The publication ended, and the request it took with it (#195).
	failpoint.Hit("submit.ended")
	return nil
}

// ErrRequestChanged is a stage's submission whose request another command
// changed while it was planned and judged: nothing was recorded, and it is
// planned and judged again under the request as it stands (#195).
var ErrRequestChanged = errors.New("the branch's request changed while this submission was judged; nothing was pushed")

// takeRequest is the hand-over of a change request to the publication a
// stage's submission records, in its first transaction (#195, "Where the
// request meets its neighbours"): the request it was judged under is
// compared with the branch's; equal, the submission's asks move to the
// publication, emptied in the request, and the publication keeps its ID;
// not equal, nothing is recorded. A person's own submit takes none, and a
// publication it goes on with keeps none.
func takeRequest(tx store.Tx, id model.BranchID, record *model.Publication) error {
	if record.RequestID == "" {
		return nil
	}
	branch, err := tx.Branch(id)
	if err != nil {
		return err
	}
	if branch.Request == nil || branch.Request.ID != record.RequestID {
		return ErrRequestChanged
	}
	request := *branch.Request
	request.Publish = model.PublishAsk{}
	return tx.SetRequest(id, &request)
}

// endRequest ends the request a finished publication took, where the
// branch's request is still that one: a request changed since, by a version
// named or another ask, is new work the publication never took, and stays.
func endRequest(tx store.Tx, id model.BranchID, record model.Publication) error {
	if record.RequestID == "" {
		return nil
	}
	branch, err := tx.Branch(id)
	if err != nil || branch.Request == nil || branch.Request.ID != record.RequestID {
		return err
	}
	return tx.SetRequest(id, nil)
}

// rerequest asks the publication's reviewers to review again, login by
// login, and records each mark: the pull request's pending requests and
// each login's own latest review are read first (ReviewerStep); a login
// with no mark is marked "may have been sent" in a transaction seen
// recorded, then asked, and its answer leaves the mark (ReviewerAnswered).
// No login is sent two requests by one publication. The receipt goes onto
// the pull request's record when the publication ends.
func (e *Engine) rerequest(ctx context.Context, branch model.BranchID, record *model.Publication) (asked, since []string, err error) {
	current, err := e.Branch(ctx, branch)
	if err != nil {
		return nil, nil, err
	}
	pr := current.PullRequest
	if pr == nil {
		return nil, nil, fmt.Errorf("%s has no pull request recorded; asking the reviewers again is still owed", current.ShortName())
	}
	ref := pullRequestRef(pr.Repository, pr.Number)
	// Follow-ups are for the commit: the fork's branch is read, through
	// the pull request, just before the call.
	status, err := e.forge().Inspect(ctx, ref)
	if err != nil {
		return nil, nil, fmt.Errorf("reading #%d before asking its reviewers again: %w; asking them is still owed, and dockhand submit %s finishes it", pr.Number, err, current.ShortName())
	}
	// This reading's own head decides, not the follow-up's earlier one:
	// where the pull request moved on since, nobody is asked or marked,
	// and the step stays owed for its commit. GitHub has no request that
	// asks only while the head is a given commit, so a push between this
	// reading and the ask itself isn't caught; that window is accepted
	// (#191, group B, Codex's review).
	if status.Head != "" && status.Head != record.Commit {
		return nil, nil, fmt.Errorf("#%d is now at %s, not %s, which this submission pushed, so its reviewers weren't asked again; asking them is still owed, and dockhand submit %s settles it", pr.Number, status.Head.Short(), record.Commit.Short(), current.ShortName())
	}
	ask := *record.Review
	ask.Reviewers = slices.Clone(ask.Reviewers)
	var send []string
	var why []string
	for i, reviewer := range ask.Reviewers {
		switch ReviewerStep(reviewer, status) {
		case ReviewerUnread:
			return nil, nil, fmt.Errorf("#%d's pending review requests couldn't be read whole; asking the reviewers again is still owed, and dockhand submit %s finishes it", pr.Number, current.ShortName())
		case ReviewedSince:
			// The answer was for an older review of theirs: dropped, and
			// said (#191, group D).
			ask.Reviewers[i].Sent = model.ReviewedAgain
			since = append(since, reviewer.Login)
		case ReviewerSend:
			ask.Reviewers[i].Sent = model.MaySent
			send = append(send, reviewer.Login)
		case ReviewerPending:
			ask.Reviewers[i].Lapsed = false
			asked = append(asked, reviewer.Login)
		case ReviewerNoLongerPending:
			// Asked, and said so: the request is no longer pending (#191,
			// Codex's review of group D).
			ask.Reviewers[i].Lapsed = true
		}
	}
	if len(send) > 0 {
		marked := *record
		marked.Review = &ask
		if err := e.rewritePublication(ctx, branch, record.ID, &marked, nil); err != nil {
			return nil, since, err
		}
		*record = marked
		account := record.By
		if account == "" {
			login, loginErr := e.forge().Login(ctx)
			if loginErr != nil {
				return asked, since, loginErr
			}
			account = login.Account
		}
		ctx = e.writeAccount(ctx, account)
		// One call a login, each with its own outcome: one call for all
		// gave every login the answer of the whole (#191, group D).
		for i, reviewer := range ask.Reviewers {
			if !slices.Contains(send, reviewer.Login) {
				continue
			}
			err := remotewrite.Ask(ctx, e.pullRequests(), reviewBasis(pr, record), ref, []string{reviewer.Login})
			sent, words := ReviewerAnswered(err)
			ask.Reviewers[i].Sent, ask.Reviewers[i].Why = sent, words
			switch sent {
			case model.SentRequest:
				asked = append(asked, reviewer.Login)
			case model.RefusedToAsk:
				why = append(why, fmt.Sprintf("GitHub refused to ask %s: %s", reviewer.Login, words))
			case model.MaySent:
				why = append(why, fmt.Sprintf("asking %s to review again got no answer (%v), so it may have been sent, and isn't sent again; ask on GitHub if it wasn't", reviewer.Login, err))
			case model.NotSent:
				why = append(why, fmt.Sprintf("asking %s: %v", reviewer.Login, err))
			}
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
	// The record moves on only with the write: one that failed leaves it
	// owing the step, its marks as recorded, for settling to read.
	if err != nil {
		return asked, since, err
	}
	*record = done
	if len(why) > 0 {
		return asked, since, errors.New(strings.Join(why, "; "))
	}
	return asked, since, nil
}

// reviewBasis is the basis a reviewer request states to the gate: Made on
// a pull request dockhand opened, or on an old record that isn't adopted,
// as today; otherwise Named, since the person answered yes to this exact
// request (docs/designs/publication-record.md at 0e0ad767, "The gate").
func reviewBasis(pr *model.PullRequest, record *model.Publication) remotewrite.Basis {
	if openedByDockhand(pr) {
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

// writeRefused reports a write answered with a refusal, so nothing of it
// was done: GitHub refusing it, the organization refusing dockhand's app,
// a login refused, or dockhand's own gate refusing it before it was sent.
// Any other error, no answer among them, may have been done.
func writeRefused(err error) bool {
	return errors.Is(err, forge.ErrRejected) || errors.Is(err, forge.ErrAppRestricted) || errors.Is(err, forge.ErrAuthentication) || refusal.KindOf(err) == refusal.Unsupported
}

// pendingAfter are the steps a submission still owes when it returns,
// with err or without, read from its record, each with its own reason
// (#191, group F): a question about the reviewers nobody answered awaits
// an answer; the first step the error stopped is failed, refused, or
// unread as the error says; each step after it waits on it; and a reviewer
// marked as maybe sent, or refused, is said so.
func (e *Engine) pendingAfter(ctx context.Context, branch model.BranchID, result Submitted, err error) []Pending {
	current, readErr := e.Branch(ctx, branch)
	if readErr != nil || current.Publication == nil {
		return nil
	}
	record := current.Publication
	var pending []Pending
	stopped := ""
	for _, step := range record.Owes {
		switch {
		case step == model.StepPush && result.Pushed:
			// This run pushed; the record says so with the opening's
			// write, which the error came before.
			continue
		case step == model.StepRerequest && record.Review != nil && record.Review.Answer == model.ReviewNotAsked:
			pending = append(pending, Pending{Step: step, Reason: PendingAnswer, Detail: "nobody was there to answer whether to ask the reviewers again"})
		case err == nil:
			pending = append(pending, Pending{Step: step, Reason: PendingWaits, Detail: "the next dockhand submit makes it"})
		case stopped != "":
			pending = append(pending, Pending{Step: step, Reason: PendingWaits, Detail: stopped})
		default:
			reason := PendingFailed
			var unsettled *unsettledError
			switch {
			case errors.As(err, &unsettled):
				reason = PendingUnread
			case writeRefused(err):
				reason = PendingRefused
			}
			pending = append(pending, Pending{Step: step, Reason: reason, Detail: err.Error()})
			stopped = "waits on " + step.Words()
		}
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

// SubmitUnderRequest plans a stage's submission and makes it with apply,
// and where another command changed the branch's request meanwhile, so
// that nothing was recorded (ErrRequestChanged), plans and judges it again
// under the request as it stands: three rounds, then it says the request
// keeps changing, nothing pushed. A request that now asks for a check that
// hasn't run stops it (#195, "Where the request meets its neighbours").
// It returns the last plan, for the caller's words.
func (e *Engine) SubmitUnderRequest(ctx context.Context, request SubmitRequest, apply func(SubmitPlan) (Submitted, error)) (SubmitPlan, Submitted, error) {
	for round := 0; ; round++ {
		plan, err := e.PlanSubmit(ctx, request)
		if err != nil {
			return plan, Submitted{}, err
		}
		if round > 0 && plan.CheckNeeded {
			return plan, Submitted{}, fmt.Errorf("%s's request now asks for a check that hasn't run; nothing was submitted, and dockhand ship -b %s runs it", plan.Branch.ShortName(), plan.Branch.ShortName())
		}
		if e.beforeStageApply != nil {
			e.beforeStageApply(round)
		}
		submitted, err := apply(plan)
		if !errors.Is(err, ErrRequestChanged) {
			return plan, submitted, err
		}
		if round == 2 {
			return plan, submitted, fmt.Errorf("%s's request kept changing while it was submitted; nothing was pushed, and dockhand ship -b %s submits it once it settles", plan.Branch.ShortName(), plan.Branch.ShortName())
		}
		if request, err = e.rejudged(ctx, request); err != nil {
			return plan, submitted, err
		}
	}
}

// rejudged is a stage's submission again under the branch's request as it
// stands now: the request's asks and ID, with the run's title, note,
// statements, head and the commit it is bound to.
func (e *Engine) rejudged(ctx context.Context, was SubmitRequest) (SubmitRequest, error) {
	branch, err := e.Branch(ctx, was.Branch.ID)
	if err != nil {
		return was, err
	}
	var request model.ChangeRequest
	if branch.Request != nil {
		request = *branch.Request
	}
	next := SubmitRequestFor(request, branch, was.Where.Unattended)
	if branch.Request == nil {
		next.RequestID = ""
	}
	next.Head, next.Bound, next.PendingCheck, next.Title, next.Note = was.Head, was.Bound, was.PendingCheck, was.Title, was.Note
	next.TestedBinaries, next.TestedVariants, next.WithdrawBinaries, next.WithdrawVariants = was.TestedBinaries, was.TestedVariants, was.WithdrawBinaries, was.WithdrawVariants
	if !was.PendingCheck && len(was.Where.On) == 0 && was.Where.Build == "" && was.Tests == "" {
		next.Where, next.Tests = WhereRequest{}, ""
	}
	return next, nil
}
