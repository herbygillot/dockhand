package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/store"
)

// Refreshed is what reading one pull request found.
type Refreshed struct {
	Branch model.Branch
	// Changes say what is new since the last reading, for people.
	Changes []string
	Err     error
}

// RefreshResult is what reading the pull requests found: each branch's,
// and Refused, where the forge refused the read altogether, as a revoked
// login is refused for every pull request: said once, not as each
// branch's, where it was said five times and status went on to exit 0 (the
// rc6 full stage, D-C3). What was read before it was refused is kept.
type RefreshResult struct {
	Branches []Refreshed
	Refused  error
	// Unread counts the pull requests a rate limit left unread, and
	// RetryAt is when it lifts; zero where none stopped the refresh.
	Unread  int
	RetryAt time.Time
}

// UnreadWords say how many pull requests a rate limit left unread, and
// when it lifts, as the day's look says its own; empty where none did.
func (r RefreshResult) UnreadWords() string {
	if r.Unread == 0 {
		return ""
	}
	return fmt.Sprintf("GitHub's rate limit prevented refresh of %s. Dockhand will retry after %s", prose.Plural(r.Unread, "pull request"), r.RetryAt.Local().Format("15:04 MST"))
}

// PullRequestRefresh is how often serve reads the pull requests, and how
// old their reading is before status reads them itself.
const PullRequestRefresh = 5 * time.Minute

// observedPullRequest is one branch's pull request as just read, or why
// it couldn't be.
type observedPullRequest struct {
	branch model.Branch
	next   model.PullRequestObservation
	read   pullRequestRead
	err    error
	// ci are its head's failed CI jobs newly classified (classifyCI).
	ci []model.CIFailure
}

// RefreshPullRequests reads every open or closed branch's pull request
// from the forge (Design v3 §6.13): its state, reviews, and checks, all of
// them first, outside any transaction, as the store's rule is, and then
// writes what was read in one (the SQL study's O8, which status and serve
// share), then settles from it the publications it can (settleRefreshed). A merged pull request marks its branch merged, and a closed one
// closed; what changed is recorded as events. A pull request that can't be
// read is reported and the rest are read; the error is the store's.
func (e *Engine) RefreshPullRequests(ctx context.Context) (RefreshResult, error) {
	var branches []model.Branch
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		branches, err = r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchOpen, model.BranchClosed}})
		return err
	}); err != nil {
		return RefreshResult{}, err
	}
	var result RefreshResult
	var observed []observedPullRequest
	read, signedIn, err := e.readPullRequests(ctx, branches)
	if refused(err) {
		return RefreshResult{Refused: err}, nil
	}
	// Signed out, each pull request is a request of GitHub's 60 an hour:
	// the open branches go, newest first, and the closed ones wait for a
	// signed-in refresh, which sees a reopening. Logged out, serve read
	// 15 closed test pull requests, spent the hour's allowance, and never
	// saw #88 close (rc5's D-C2).
	// And each is one request, its state and head alone: reviews, checks,
	// and CI's logs cost four or more a pull request a refresh, the
	// hour's 60 in an hour's refreshes of one, and #88 and #89 were never
	// read (rc5's D-C2 again). The review and checks recorded stay as
	// they were. It stops anonymousReserve short of the hour's allowance,
	// as GitHub says it, so a person's own command still has some.
	budget := -1
	var lifts time.Time
	if !signedIn {
		branches, read = openNewestFirst(branches), e.observeState
		if reader, ok := e.forge().(forge.AllowanceReader); ok {
			if left, resets, err := reader.RequestsLeft(ctx); err == nil {
				budget, lifts = max(left-anonymousReserve, 0), resets
			}
		}
	}
	for i, branch := range branches {
		if branch.PullRequest == nil {
			continue
		}
		if budget == 0 {
			for _, rest := range branches[i:] {
				if rest.PullRequest != nil {
					result.Unread++
				}
			}
			result.RetryAt = lifts
			break
		}
		if budget > 0 {
			budget--
		}
		got, err := read(ctx, branch.PullRequest)
		next := got.next
		if refused(err) {
			result.Refused = err
			break
		}
		// The rest would be refused alike: they wait for the limit.
		if limit := new(forge.RateLimitError); errors.As(err, &limit) {
			for _, rest := range branches[i:] {
				if rest.PullRequest != nil {
					result.Unread++
				}
			}
			result.RetryAt = limit.RetryAt
			break
		}
		o := observedPullRequest{branch: branch, next: next, read: got, err: err}
		if err == nil && signedIn {
			o.ci = e.classifyCI(ctx, branch, next)
		}
		observed = append(observed, o)
	}
	others := e.othersPullRequests(ctx, observed)
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		result.Branches = nil
		for _, o := range observed {
			refreshed := Refreshed{Branch: o.branch, Err: o.err}
			if o.err == nil {
				if err := recordPullRequest(tx, o.branch, o.next, &refreshed); err != nil {
					return err
				}
				if others[o.branch.ID] {
					if err := markFound(tx, o.branch.ID, o.branch.PullRequest.Number); err != nil {
						return err
					}
				}
				for _, failure := range o.ci {
					if err := tx.AddCIFailure(failure); err != nil {
						return err
					}
				}
			}
			result.Branches = append(result.Branches, refreshed)
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	// What was read settles the publications it can (#191, group A).
	return result, e.settleRefreshed(ctx, observed)
}

// othersPullRequests are the branches whose old record, with no
// provenance, is of a pull request GitHub names another author of than the
// account dockhand acts as: each becomes found at this read. The author is
// only ever used to withdraw Made, never to state it (docs/designs/
// publication-record.md at 0e0ad767, "Existing databases and branches"). The login is
// asked for only where such a record was read.
func (e *Engine) othersPullRequests(ctx context.Context, observed []observedPullRequest) map[model.BranchID]bool {
	others := map[model.BranchID]bool{}
	account := ""
	for _, o := range observed {
		pr := o.branch.PullRequest
		if o.err != nil || pr == nil || pr.Adopted || pr.Provenance != "" || o.next.Author == "" {
			continue
		}
		if account == "" {
			login, err := e.forge().Login(ctx)
			if err != nil || login.Account == "" {
				return others
			}
			account = login.Account
		}
		if !strings.EqualFold(o.next.Author, account) {
			others[o.branch.ID] = true
		}
	}
	return others
}

// markFound records a branch's pull request as found, where it is still
// the one numbered so and has no provenance.
func markFound(tx store.Tx, id model.BranchID, number int) error {
	branch, err := tx.Branch(id)
	if err != nil || branch.PullRequest == nil || branch.PullRequest.Number != number || branch.PullRequest.Provenance != "" {
		return err
	}
	pr := *branch.PullRequest
	pr.Provenance = model.Found
	branch.PullRequest = &pr
	return tx.UpdateBranch(branch)
}

// anonymousReserve is the requests a signed-out refresh leaves of the
// hour's allowance.
const anonymousReserve = 10

// observeState reads a pull request's state and head alone, one request,
// for a refresh without a login: the review and checks recorded stay as
// they were, since reading them is four requests more. Its opening,
// author, and description come with the one request, so a merge's
// fitness is read signed out as well, its approvals as recorded.
func (e *Engine) observeState(ctx context.Context, pr *model.PullRequest) (pullRequestRead, error) {
	observed, err := e.forge().Observe(ctx, pullRequestRef(pr.Repository, pr.Number))
	if errors.Is(err, forge.ErrNotFound) {
		return pullRequestRead{}, fmt.Errorf("#%d was not found", pr.Number)
	}
	if err != nil {
		return pullRequestRead{}, err
	}
	next := e.observation(observed.PullRequest, forge.PullRequestStatus{})
	if was := pr.Observed; was != nil && next.State == model.PullRequestOpen {
		next.Draft, next.Review, next.ChangesRequestedBy, next.ReviewedAt, next.ApprovedBy, next.CommentedBy = was.Draft, was.Review, was.ChangesRequestedBy, was.ReviewedAt, was.ApprovedBy, was.CommentedBy
		next.CommentedAt = was.CommentedAt
		next.Checks, next.Failing = was.Checks, was.Failing
	}
	return pullRequestRead{next: next, pr: observed.PullRequest}, nil
}

// openNewestFirst are the open branches with a pull request, the newest
// pull request first.
func openNewestFirst(branches []model.Branch) []model.Branch {
	var open []model.Branch
	for _, branch := range branches {
		if branch.State == model.BranchOpen && branch.PullRequest != nil {
			open = append(open, branch)
		}
	}
	slices.SortStableFunc(open, func(a, b model.Branch) int { return cmp.Compare(b.PullRequest.Number, a.PullRequest.Number) })
	return open
}

// refused reports a read the forge refused altogether, as it refuses a
// rejected or ended login for every pull request.
func refused(err error) bool {
	return errors.Is(err, github.ErrAuthentication) || errors.Is(err, github.ErrLoginEnded)
}

// readPullRequests reads the branches' pull requests in one read over all
// of them, where the forge can (forge.PullRequestReader, GitHub's GraphQL
// API), and gives what reads each: what that read found, or
// observePullRequest's own read for one it couldn't say all of, or where
// the forge can't read many at once. A read over all of them that fails,
// but for a refusal, leaves each to its own. signedIn is false where the
// forge has no credentials, so each read is one of its anonymous ones.
func (e *Engine) readPullRequests(ctx context.Context, branches []model.Branch) (_ func(context.Context, *model.PullRequest) (pullRequestRead, error), signedIn bool, _ error) {
	reader, ok := e.forge().(forge.PullRequestReader)
	if !ok {
		return e.readPullRequest, true, nil
	}
	var refs []forge.PullRequestRef
	for _, branch := range branches {
		if pr := branch.PullRequest; pr != nil {
			refs = append(refs, pullRequestRef(pr.Repository, pr.Number))
		}
	}
	if len(refs) == 0 {
		return e.readPullRequest, true, nil
	}
	readings, err := reader.ReadPullRequests(ctx, refs)
	switch {
	// GitHub's GraphQL API takes only a signed-in request, and a public
	// pull request is read without one, a request at a time.
	case errors.Is(err, github.ErrNoCredentials):
		return e.readPullRequest, false, nil
	case err != nil:
		return e.readPullRequest, true, err
	}
	byNumber := map[forge.PullRequestRef]forge.PullRequestReading{}
	for _, reading := range readings {
		byNumber[reading.Ref] = reading
	}
	return func(ctx context.Context, pr *model.PullRequest) (pullRequestRead, error) {
		reading, ok := byNumber[pullRequestRef(pr.Repository, pr.Number)]
		switch {
		case !ok:
			return e.readPullRequest(ctx, pr)
		case reading.Partial:
			// Keep the conversation from the partial read if the fallback
			// cannot read it itself (#248).
			if reading.Status.CommentsRead {
				copyPR := *pr
				var prior model.PullRequestObservation
				if pr.Observed != nil {
					prior = *pr.Observed
				}
				prior.CommentedBy, prior.CommentedAt, prior.CommentsRead = reading.Status.CommentedBy, reading.Status.CommentedAt, true
				copyPR.Observed = &prior
				return e.readPullRequest(ctx, &copyPR)
			}
			return e.readPullRequest(ctx, pr)
		case !reading.Found:
			return pullRequestRead{}, fmt.Errorf("#%d was not found", pr.Number)
		}
		next := e.observation(reading.PullRequest, reading.Status)
		if !reading.Status.CommentsRead && pr.Observed != nil {
			next.CommentedBy, next.CommentedAt, next.CommentsRead = pr.Observed.CommentedBy, pr.Observed.CommentedAt, pr.Observed.CommentsRead
		}
		got := pullRequestRead{next: next, pr: reading.PullRequest}
		if reading.PullRequest.State == forge.PullRequestOpen {
			got.status = &reading.Status
		}
		return got, nil
	}, true, nil
}

// observation is a pull request's reading as the branch records it: its
// state and head, and while it's open, its draft, review, and checks, and
// its opening, author, mentions, and approvals, which its merge's fitness
// reads.
func (e *Engine) observation(now forge.PullRequest, status forge.PullRequestStatus) model.PullRequestObservation {
	next := model.PullRequestObservation{State: model.PullRequestState(now.State), Head: model.ObjectID(now.RemoteHead), Review: model.ReviewNone, Checks: model.ChecksNone, At: e.now()}
	if next.State != model.PullRequestOpen {
		return next
	}
	next.Draft, next.Review, next.Failing, next.ChangesRequestedBy, next.ReviewedAt = status.Draft, model.PullRequestReview(status.Review), status.Checks.Failing, status.ChangesRequestedBy, status.ReviewedAt
	next.Opened, next.Author, next.Mentioned, next.ApprovedBy, next.CommentedBy = now.CreatedAt, now.Author, now.Mentions, status.ApprovedBy, status.CommentedBy
	next.CommentedAt, next.CommentsRead = status.CommentedAt, status.CommentsRead
	next.Checks = checksOf(status.Checks)
	next.Conflicts = status.Mergeable == "no"
	return next
}

// observePullRequest reads a pull request, its reviews and checks with it
// while it's open.
func (e *Engine) observePullRequest(ctx context.Context, pr *model.PullRequest) (model.PullRequestObservation, error) {
	got, err := e.readPullRequest(ctx, pr)
	return got.next, err
}

// pullRequestRead is a pull request as a refresh read it: what its branch
// records of it, and GitHub's answer, which settles a publication for it
// (settleRefreshed). status is nil where its draft state wasn't read.
type pullRequestRead struct {
	next   model.PullRequestObservation
	pr     forge.PullRequest
	status *forge.PullRequestStatus
}

// readPullRequest is observePullRequest with GitHub's answer kept.
func (e *Engine) readPullRequest(ctx context.Context, pr *model.PullRequest) (pullRequestRead, error) {
	ref := pullRequestRef(pr.Repository, pr.Number)
	observed, err := e.forge().Observe(ctx, ref)
	if errors.Is(err, forge.ErrNotFound) {
		return pullRequestRead{}, fmt.Errorf("#%d was not found", pr.Number)
	}
	if err != nil {
		return pullRequestRead{}, err
	}
	if observed.PullRequest.State != forge.PullRequestOpen {
		return pullRequestRead{next: e.observation(observed.PullRequest, forge.PullRequestStatus{}), pr: observed.PullRequest}, nil
	}
	status, err := e.forge().Inspect(ctx, ref)
	if err != nil {
		return pullRequestRead{}, err
	}
	next := e.observation(observed.PullRequest, status)
	// An unread conversation cannot erase the earlier answer (#248).
	if !status.CommentsRead && pr.Observed != nil {
		next.CommentedBy, next.CommentedAt, next.CommentsRead = pr.Observed.CommentedBy, pr.Observed.CommentedAt, pr.Observed.CommentsRead
	}
	return pullRequestRead{next: next, pr: observed.PullRequest, status: &status}, nil
}

// checksOf is how a pull request's checks stand, from their counts.
func checksOf(checks forge.CheckSummary) model.PullRequestChecks {
	switch {
	case checks.Failed > 0:
		return model.ChecksFailing
	case checks.Pending > 0:
		return model.ChecksPending
	case checks.Total > 0:
		return model.ChecksPassing
	}
	return model.ChecksNone
}

// recordPullRequest writes a pull request's new reading on its branch, as
// the branch stands in the transaction, with what changed since its last
// reading as events, and the branch's state as the pull request's moves
// it: merged, closed, or open again.
func recordPullRequest(tx store.Tx, branch model.Branch, next model.PullRequestObservation, refreshed *Refreshed) error {
	pr := branch.PullRequest
	current, err := tx.Branch(branch.ID)
	if err != nil || current.PullRequest == nil || current.PullRequest.Number != pr.Number || current.PullRequest.Repository != pr.Repository {
		return err
	}
	// A recorded push to this pull request after the comment answers it (#248).
	if next.CommentsRead && next.CommentedBy != "" && !next.CommentedAt.IsZero() && current.PullRequest.PushedAt.After(next.CommentedAt) {
		next.CommentedBy, next.CommentedAt = "", time.Time{}
	}
	previous := pr.Observed
	if previous == nil {
		previous = &model.PullRequestObservation{}
	}
	if previous.State != next.State {
		refreshed.Changes = append(refreshed.Changes, fmt.Sprintf("#%d is %s", pr.Number, next.State))
	}
	if next.State == model.PullRequestOpen {
		if previous.Review != next.Review && next.Review != model.ReviewNone {
			refreshed.Changes = append(refreshed.Changes, fmt.Sprintf("#%d: %s", pr.Number, reviewWords(next.Review)))
		}
		if next.CommentedBy != "" && previous.CommentedBy != next.CommentedBy {
			refreshed.Changes = append(refreshed.Changes, fmt.Sprintf("#%d: @%s commented", pr.Number, next.CommentedBy))
		}
		if previous.Checks != next.Checks && (next.Checks == model.ChecksFailing || next.Checks == model.ChecksPassing) {
			refreshed.Changes = append(refreshed.Changes, fmt.Sprintf("#%d: CI %s", pr.Number, next.Checks))
		}
	}
	current.PullRequest.Observed = &next
	current.PullRequest.Draft = next.Draft
	was := current.State
	switch {
	case next.State == model.PullRequestMerged && current.State.CanBecome(model.BranchMerged):
		current.State = model.BranchMerged
	case next.State == model.PullRequestClosed && current.State == model.BranchOpen:
		current.State = model.BranchClosed
	case next.State == model.PullRequestOpen && current.State == model.BranchClosed:
		current.State = model.BranchOpen
	}
	// It ended when GitHub was seen to say so; the store records the time
	// it's given, and invents none (the architecture review's X5).
	if was == model.BranchOpen && current.State != model.BranchOpen {
		current.EndedAt = next.At
	}
	if err := tx.UpdateBranch(current); err != nil {
		return err
	}
	refreshed.Branch = current
	for _, change := range refreshed.Changes {
		if _, err := tx.AppendEvent(model.Event{At: next.At, Branch: branch.ID, Kind: model.EventPullRequestObserved, Level: model.LevelInfo, Message: branch.ShortName() + ": " + change}); err != nil {
			return err
		}
	}
	return nil
}

// refresh reads one branch's pull request and writes what it found, as
// RefreshPullRequests does for every branch's.
func (e *Engine) refresh(ctx context.Context, branch model.Branch) (Refreshed, error) {
	next, err := e.observePullRequest(ctx, branch.PullRequest)
	if err != nil {
		return Refreshed{Branch: branch}, err
	}
	refreshed := Refreshed{Branch: branch}
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		refreshed.Changes = nil
		return recordPullRequest(tx, branch, next, &refreshed)
	})
	return refreshed, err
}

// followedPullRequests are the open branches' pull requests that were open
// when last read, or haven't been read, as "#35353", by number.
func (e *Engine) followedPullRequests(ctx context.Context) []string {
	var open []string
	_ = e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		branches, err := r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchOpen}})
		for _, branch := range branches {
			if pr := branch.PullRequest; pr != nil && (pr.Observed == nil || pr.Observed.State == model.PullRequestOpen) {
				open = append(open, fmt.Sprintf("#%d", pr.Number))
			}
		}
		return err
	})
	return open
}

// PullRequestsStale reports whether the open branches' pull requests were
// last read longer ago than serve reads them, or never: status then reads
// them itself, where it said "CI not read yet" until --refresh. One with
// no pull request, or none open, isn't stale.
func (e *Engine) PullRequestsStale(ctx context.Context) (bool, error) {
	stale := false
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		branches, err := r.Branches(store.BranchFilter{States: []model.BranchState{model.BranchOpen}})
		for _, branch := range branches {
			pr := branch.PullRequest
			if pr != nil && (pr.Observed == nil || e.now().Sub(pr.Observed.At) > PullRequestRefresh) {
				stale = true
			}
		}
		return err
	})
	return stale, err
}

func reviewWords(review model.PullRequestReview) string {
	switch review {
	case model.ReviewChangesRequested:
		return "changes requested"
	case model.ReviewApproved:
		return "approved"
	}
	return "no review yet"
}

// SomeoneElsePushed reports whether the pull request holds a commit that
// dockhand did not push and the branch does not have.
func (s BranchStatus) SomeoneElsePushed() bool {
	pr := s.Branch.PullRequest
	if pr == nil || pr.Observed == nil || pr.Observed.Head == "" || pr.Pushed == "" {
		return false
	}
	return pr.Observed.Head != pr.Pushed && string(pr.Observed.Head) != s.Head
}

// Failing lists the pull request's failing checks, sorted.
func (s BranchStatus) Failing() []string {
	if s.Branch.PullRequest == nil || s.Branch.PullRequest.Observed == nil {
		return nil
	}
	failing := slices.Clone(s.Branch.PullRequest.Observed.Failing)
	slices.Sort(failing)
	return failing
}

// staleReadTimeout bounds a read of the pull requests a command makes by
// itself, so a slow or unreachable GitHub delays it only so long.
const staleReadTimeout = 20 * time.Second

// RefreshStalePullRequests reads the pull requests where what's recorded
// of them is older than serve reads them (PullRequestsStale), within
// staleReadTimeout, as a plain status does by itself, where it said "CI
// not read yet" until --refresh: the one stale-read rule (R3-39). read is
// whether it read; a failure to tell whether they're stale reads nothing.
func (e *Engine) RefreshStalePullRequests(ctx context.Context) (result RefreshResult, read bool, err error) {
	if stale, err := e.PullRequestsStale(ctx); err != nil || !stale {
		return RefreshResult{}, false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, staleReadTimeout)
	defer cancel()
	result, err = e.RefreshPullRequests(ctx)
	return result, true, err
}
