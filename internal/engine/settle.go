package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/store"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/macports/prdescription"
	"github.com/herbygillot/dockhand/internal/model"
)

// PublicationReading is what settling read of the fork and GitHub for a
// publication, under its head's lock (docs/designs/publication-record.md,
// "The steps"). A read that failed, or came back cut short, is not read:
// it settles nothing.
type PublicationReading struct {
	// HeadRead says the fork's head branch was read: HeadExists, at
	// HeadAt.
	HeadRead, HeadExists bool
	HeadAt               model.ObjectID
	// FoundRead says the pull request was looked for by Head and Base, in
	// every state, an open one first and else the newest: Found it was,
	// as PullRequest.
	FoundRead, Found bool
	PullRequest      forge.PullRequest
	// StatusRead says its status was read, whose Draft settles draft and
	// ready.
	StatusRead bool
	Status     forge.PullRequestStatus
}

// Settlement is what settling found: the steps found done, folded into
// the pull request's record; those still owed; the pull request the
// publication is now known by, and how; the description dockhand has
// written of it; and whether the publication ends, and why.
type Settlement struct {
	Done, Owes []model.PublicationStep
	// Number is the pull request found or recorded, 0 where none, and
	// Provenance how it is known: found, where a read found it.
	Number     int
	Provenance model.Provenance
	// Created is GitHub's time for the found pull request's creation,
	// zero where none was found or GitHub didn't give it.
	Created time.Time
	// Body is what dockhand has written of the description, where the
	// edit or the opening landed in part or whole (prdescription.Landed);
	// empty where nothing of it is known to have landed.
	Body string
	// Ended is the publication ending, with nothing more it can do for its
	// commit: Why says how, for the person.
	Ended bool
	Why   string
	// Unread are the reads that failed, each named for the person: the
	// publication stays as it is for what they would have settled.
	Unread []string
}

// Settle reads a publication against what was read of the fork and
// GitHub, and says what is done, what is owed, and whether it ends. It
// only reads (the design's rule 3): nothing is done again until a read
// shows it isn't done (rule 1), and a read that failed settles nothing. A
// pull request merged or closed was still opened: what landed is folded,
// then the publication ends. A fork branch at a third commit ends the
// steps that were for Commit, after what landed is folded.
func Settle(publication model.Publication, recorded *model.PullRequest, reading PublicationReading) Settlement {
	s := Settlement{Number: publication.Number}
	owe := func(step model.PublicationStep) { s.Owes = append(s.Owes, step) }
	done := func(step model.PublicationStep) { s.Done = append(s.Done, step) }
	unread := func(what string) {
		if !slices.Contains(s.Unread, what) {
			s.Unread = append(s.Unread, what)
		}
	}
	// The pull request: the one recorded, else one found above Earlier.
	var pr *forge.PullRequest
	if reading.FoundRead && reading.Found {
		found := reading.PullRequest
		switch {
		case publication.Number != 0 && found.Ref.Number == publication.Number:
			pr = &found
		case publication.Number == 0 && found.Ref.Number > publication.Earlier:
			pr, s.Number, s.Provenance = &found, found.Ref.Number, model.Found
		}
	}
	if pr != nil {
		s.Created = pr.CreatedAt
	}
	terminal := pr != nil && pr.State != forge.PullRequestOpen
	for _, step := range publication.Owes {
		switch step {
		case model.StepPush:
			switch {
			case !reading.HeadRead:
				unread("the fork's branch")
				owe(step)
			case reading.HeadExists && reading.HeadAt == publication.Commit:
				done(step)
			case !reading.HeadExists && publication.Expected == "", reading.HeadExists && reading.HeadAt == publication.Expected:
				owe(step)
			default:
				s.Ended, s.Why = true, fmt.Sprintf("the fork's branch is at %s, which this submission didn't push", reading.HeadAt.Short())
			}
		case model.StepOpen:
			switch {
			case !reading.FoundRead:
				unread("the pull request")
				owe(step)
			case pr != nil:
				done(step)
			default:
				owe(step)
			}
		case model.StepEdit:
			switch {
			case pr == nil && !reading.FoundRead:
				unread("the pull request")
				owe(step)
			case pr != nil && pr.Title == publication.Title && sameText(pr.Body, publication.Body):
				done(step)
			default:
				owe(step)
			}
		case model.StepDraft, model.StepReady:
			draft := step == model.StepDraft
			switch {
			case !reading.StatusRead:
				unread("the pull request's draft state")
				owe(step)
			case reading.Status.Draft == draft:
				done(step)
			default:
				owe(step)
			}
		default:
			// A reviewer request is settled login by login (settleReviewer).
			owe(step)
		}
	}
	// What landed of the description, where the pull request is read.
	if pr != nil && publication.Body != "" {
		was := ""
		if recorded != nil {
			was = recorded.Body
		}
		if landed := prdescription.Landed(pr.Body, publication.Body, publication.Written, was); landed != was {
			s.Body = landed
		}
	}
	if terminal {
		s.Ended, s.Why = true, fmt.Sprintf("#%d was %s", pr.Ref.Number, pr.State)
	}
	if s.Ended {
		// Its remaining steps were for Commit; what landed is folded first.
		s.Owes = nil
	}
	return s
}

// sameText reports two descriptions the same, GitHub's line endings
// aside.
func sameText(observed, sent string) bool {
	return prdescription.Landed(observed, sent, "x", "") == "x"
}

// Pending is a step a submission still owes when it returns, and why, as
// submit's and ship's --json give it (`pending`): never by the step's
// internal name alone in a message.
type Pending struct {
	Step   model.PublicationStep
	Reason PendingReason
	// Detail is the error, GitHub's words, or the read that failed.
	Detail string
}

// PendingReason tells apart why a step is still owed.
type PendingReason string

const (
	// PendingFailed is a step that failed, with its error.
	PendingFailed PendingReason = "failed"
	// PendingAnswer awaits the person's answer, as the reviewers' does
	// with nobody there.
	PendingAnswer PendingReason = "awaits-answer"
	// PendingMaySent may have been sent, and isn't sent again.
	PendingMaySent PendingReason = "may-have-been-sent"
	// PendingRefused is a step GitHub refused, with its words.
	PendingRefused PendingReason = "refused"
	// PendingUnread couldn't be read, and settles on a later read.
	PendingUnread PendingReason = "unread"
)

// FailsCommand reports whether a pending step fails the command: a step
// that failed, or GitHub refused. One that awaits the person's answer, or
// may have been sent, doesn't; a ready asked and not done is failed.
func (p Pending) FailsCommand() bool {
	return p.Reason == PendingFailed || p.Reason == PendingRefused
}

// settlePublication accounts for what an earlier submission of the branch
// left, under its head's lock, before a new one is planned: it reads the
// fork's branch, the pull request by head and base, and its draft state,
// settles what each step left (Settle), and in one transaction folds what
// is done into the pull request's record, the description that landed,
// serve's row, and the reviewer receipt, then ends the publication. It
// only reads the fork and GitHub. A read that fails leaves it as it is,
// and the plan is refused with the read named. What the person asked of
// it that still applies is returned, for the plan to carry.
func (e *Engine) settlePublication(ctx context.Context, worktree *git.Repository, branch model.Branch, pushURL string) (model.Branch, *model.Publication, []model.PublicationStep, error) {
	record := branch.Publication
	if record == nil {
		return branch, nil, nil, nil
	}
	headRepository, headBranch, _ := strings.Cut(record.Head, ":")
	baseRepository, baseBranch, _ := strings.Cut(record.Base, ":")
	var settled Settlement
	var unread error
	err := worktree.WithHeadLock(ctx, record.Head, func(ctx context.Context) error {
		current, err := e.Branch(ctx, branch.ID)
		if err != nil {
			return err
		}
		if current.Publication == nil || current.Publication.ID != record.ID {
			branch = current
			record = nil
			return nil
		}
		var reading PublicationReading
		if pushURL != "" {
			if head, err := worktree.RemoteHead(ctx, pushURL, headBranch); err == nil {
				reading.HeadRead, reading.HeadExists, reading.HeadAt = true, head.Exists, model.ObjectID(head.Object)
			}
		}
		found, err := e.forge().Find(ctx, forge.PullRequestQuery{Repository: baseRepository, HeadRepository: headRepository, HeadBranch: headBranch, BaseBranch: baseBranch})
		if err == nil {
			reading.FoundRead, reading.Found, reading.PullRequest = true, found.Found, found.PullRequest
		}
		number := record.Number
		if reading.Found && number == 0 && found.PullRequest.Ref.Number > record.Earlier {
			number = found.PullRequest.Ref.Number
		}
		if number != 0 {
			if status, err := e.forge().Inspect(ctx, pullRequestRef(baseRepository, number)); err == nil {
				reading.StatusRead, reading.Status = true, status
			}
		}
		settled = Settle(*record, current.PullRequest, reading)
		if len(settled.Unread) > 0 && !settled.Ended {
			unread = fmt.Errorf("an earlier submission of %s couldn't be accounted for: %s couldn't be read; nothing was done, and dockhand submit tries again", branch.ShortName(), prose.And(settled.Unread))
			return nil
		}
		return e.rewritePublication(ctx, branch.ID, record.ID, nil, func(tx store.Tx) error {
			return e.foldSettlement(tx, branch.ID, *record, settled, baseRepository)
		})
	})
	if err != nil {
		return branch, nil, nil, err
	}
	if unread != nil {
		return branch, nil, nil, unread
	}
	if record == nil {
		return branch, nil, nil, nil
	}
	branch, err = e.Branch(ctx, branch.ID)
	if settled.Ended {
		return branch, nil, nil, err
	}
	carried := *record
	carried.Owes = settled.Owes
	return branch, &carried, settled.Done, err
}

// foldSettlement writes what settling found into the branch's record,
// before the publication goes: each step found done into the pull
// request, the description's parts that landed, serve's row with the pull
// request and GitHub's creation time, and every reviewer mark other than
// not sent as the receipt (docs/designs/publication-record.md, "Ending a
// publication").
func (e *Engine) foldSettlement(tx store.Tx, id model.BranchID, record model.Publication, settled Settlement, repository string) error {
	branch, err := tx.Branch(id)
	if err != nil {
		return err
	}
	pr := branch.PullRequest
	found := false
	if settled.Number != 0 && (pr == nil || pr.Number != settled.Number) {
		pr = &model.PullRequest{Repository: repository, Number: settled.Number, Head: record.Head, Provenance: settled.Provenance}
		found = true
	}
	if pr != nil {
		for _, step := range settled.Done {
			switch step {
			case model.StepPush:
				// A push to a pull request already recorded is left for
				// the next submission's record, which says it in the
				// timeline after its update, as a clean run does.
				if found {
					pr.Pushed = record.Commit
				}
			case model.StepDraft:
				pr.Draft = true
			case model.StepReady:
				pr.Draft = false
			}
		}
		if settled.Body != "" {
			pr.Body = settled.Body
		}
		if record.Review != nil && slices.ContainsFunc(record.Review.Reviewers, func(r model.Reviewer) bool { return r.Sent != model.NotSent }) {
			pr.Rerequested = &model.ReviewReceipt{Head: record.Commit, Reviewers: record.Review.Reviewers}
		}
		branch.PullRequest = pr
		if err := tx.UpdateBranch(branch); err != nil {
			return err
		}
	}
	if !record.Served {
		return nil
	}
	rows, err := tx.ServeOpenings()
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Head != record.Head || row.Base != record.Base || row.Number != 0 {
			continue
		}
		switch {
		case settled.Number != 0:
			if charged, ok := row.FoundFor(settled.Number, settled.Created); ok {
				return tx.UpdateServeOpening(charged)
			}
		case row.Tried.IsZero():
			// A place never tried is released with its publication.
			return tx.ReleaseServeOpening(row.ID)
		}
	}
	return nil
}

// endPublicationFor ends a branch's unfinished submission before the
// branch is archived or forgotten: what each step left is read and
// written down, then it goes (docs/designs/publication-record.md, "Ending
// a publication"). Where a read it needs fails, it stays, and the verb is
// refused with nothing done.
func (e *Engine) endPublicationFor(ctx context.Context, branch model.Branch, verb string) error {
	current, err := e.Branch(ctx, branch.ID)
	if err != nil || current.Publication == nil {
		return err
	}
	worktree := e.Repo
	if current.Worktree != "" && exists(current.Worktree) {
		if worktree, err = e.worktree(ctx, current); err != nil {
			return err
		}
	}
	pushURL := ""
	if fork, err := e.Fork(ctx, ""); err == nil {
		pushURL = fork.PushURL
	}
	if _, _, _, err := e.settlePublication(ctx, worktree, current, pushURL); err != nil {
		return fmt.Errorf("%w; %s wasn't %s", err, current.ShortName(), verb)
	}
	return nil
}
