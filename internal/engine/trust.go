package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/refusal"
	"github.com/herbygillot/dockhand/internal/store"
)

// The trust rule (the direction synthesis's "Before D", the person's word
// of 2026-10-02; Then's item 12; asked first since #162, the person's "Ask
// first" of 2026-11-10 and "Each head, v0.9.0" of 2026-11-11): MacPorts'
// evaluator runs a Portfile's Tcl as the person, on the host that holds
// the GitHub login, so dockhand evaluates on this Mac a Portfile another
// wrote only where the person said yes, for the pull request and the
// author's commit. The engine never asks: the command asks and records
// the answer (AllowEvaluation), and the engine reads the record. Every tree
// staged for evaluation comes through one door (workspace.Open and
// Acquire), which refuses work marked as another's and not allowed
// (evaluating), naming the command that allows it. Nothing unattended
// asks: serve drives a check of another's branch only where its author's
// commit is allowed, and a local check of an adopted branch, which would
// run it as root, is refused whatever the answer (localAllowed).

// OthersPortfiles is whose a branch's Portfiles are where they aren't the
// person's, as an adopted pull request's are its author's: "@alice's", or
// "#4711's author's" where the author wasn't kept; false for the person's
// own.
func OthersPortfiles(branch model.Branch) (string, bool) {
	pr := branch.PullRequest
	if pr == nil || !pr.Adopted {
		return "", false
	}
	return pullRequestWhose(pr.Number, pr.Author), true
}

// pullRequestWhose words whose a pull request's Portfiles are.
func pullRequestWhose(number int, author string) string {
	if author == "" {
		return fmt.Sprintf("#%d's author's", number)
	}
	return "@" + author + "'s"
}

// authorHead is the author's commit an adopted branch's Portfiles are
// evaluated at, which the person's answer is for: the commit it was
// adopted at or last moved to; Pushed for one adopted before that was
// kept apart.
func authorHead(pr *model.PullRequest) model.ObjectID {
	if pr.AuthorHead != "" {
		return pr.AuthorHead
	}
	return pr.Pushed
}

// NotAllowedError is evaluation refused at the door: another person's
// Portfiles, at an author's commit the person hasn't allowed.
type NotAllowedError struct {
	Number int
	Whose  string
	Head   model.ObjectID
}

func (e *NotAllowedError) Error() string {
	return fmt.Sprintf("#%d's Portfiles are %s, and you haven't allowed dockhand to evaluate them on this Mac at %s; dockhand adopt --pr %d --evaluate allows it", e.Number, e.Whose, e.Head.Short(), e.Number)
}

// NotAllowedFor is the refusal of evaluating an adopted branch's Portfiles
// at its author's commit, as the door gives it: what a command that asked
// and got a No stops with.
func NotAllowedFor(branch model.Branch) error {
	whose, _ := OthersPortfiles(branch)
	return (&NotAllowedError{Number: branch.PullRequest.Number, Whose: whose, Head: authorHead(branch.PullRequest)}).refusal()
}

// refusal is the error as a refusal with its Next.
func (e *NotAllowedError) refusal() error {
	return refusal.Wrap(refusal.Failed, e, "%s", e.Error()).Then(refusal.Next{Verb: refusal.VerbAdopt, Args: []string{"--pr", strconv.Itoa(e.Number), "--evaluate"}})
}

// EvaluationConsents are the person's answers for pull request number,
// newest first.
func (e *Engine) EvaluationConsents(ctx context.Context, number int) ([]model.EvaluationConsent, error) {
	var consents []model.EvaluationConsent
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) (err error) {
		consents, err = r.EvaluationConsents(e.PullRequestRepository(), number)
		return err
	})
	return consents, err
}

// Allowed reports whether the person allowed pull request number's
// Portfiles to be evaluated at the author's commit head.
func (e *Engine) Allowed(ctx context.Context, number int, head model.ObjectID) (bool, error) {
	consents, err := e.EvaluationConsents(ctx, number)
	for _, consent := range consents {
		if consent.Head == head {
			return true, err
		}
	}
	return false, err
}

// AllowEvaluation records the person's yes to evaluating pull request
// number's Portfiles at the author's commit head.
func (e *Engine) AllowEvaluation(ctx context.Context, number int, head model.ObjectID) error {
	if head == "" {
		return errors.New("an author's commit is required to allow its evaluation")
	}
	return e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		return tx.AllowEvaluation(model.EvaluationConsent{Repository: e.PullRequestRepository(), Number: number, Head: head, At: e.now()})
	})
}

// EvaluationAllowed reports whether a branch's Portfiles may be evaluated
// on this Mac: the person's own, always; an adopted pull request's, where
// the person allowed its author's commit. The person's own commits above
// it on the branch are theirs, and ask nothing.
func (e *Engine) EvaluationAllowed(ctx context.Context, branch model.Branch) (bool, error) {
	pr := branch.PullRequest
	if _, others := OthersPortfiles(branch); !others {
		return true, nil
	}
	return e.Allowed(ctx, pr.Number, authorHead(pr))
}

// AllowAdopted records the person's yes for an adopted branch's author's
// commit.
func (e *Engine) AllowAdopted(ctx context.Context, branch model.Branch) error {
	if _, others := OthersPortfiles(branch); !others {
		return nil
	}
	return e.AllowEvaluation(ctx, branch.PullRequest.Number, authorHead(branch.PullRequest))
}

// evaluating marks the work under ctx as evaluating a branch's Portfiles:
// the person's own, marked as theirs, or another's, which the door says
// once where the person allowed the author's commit and refuses
// otherwise. The refusal is returned too, for work that stops before it
// evaluates, as a change record must, which would otherwise record it as
// an evaluation that failed.
func (e *Engine) evaluating(ctx context.Context, branch model.Branch) (context.Context, error) {
	whose, others := OthersPortfiles(branch)
	if !others {
		return workspace.Yours(ctx, branch.ShortName()), nil
	}
	owner := workspace.Owner{Whose: whose, Of: branch.ShortName()}
	allowed, err := e.EvaluationAllowed(ctx, branch)
	switch {
	case err != nil:
		owner.Refused = err
	case !allowed:
		owner.Refused = (&NotAllowedError{Number: branch.PullRequest.Number, Whose: whose, Head: authorHead(branch.PullRequest)}).refusal()
	}
	return workspace.WithOwner(ctx, owner), owner.Refused
}

// marked is evaluating for a read that goes on where the branch's
// Portfiles may not be evaluated, as status and its words do: the context
// carries the refusal, so the door refuses each evaluation it would make,
// and the read says what it couldn't. Status never asks (#162).
func (e *Engine) marked(ctx context.Context, branch model.Branch) context.Context {
	ctx, _ = e.evaluating(ctx, branch)
	return ctx
}

// evaluatingRevision is evaluating for a revision of a branch, as a check's
// plan and its driver read it: consent is asked for the author's commit
// behind the revision, not the branch's now. A revision that holds the
// branch's author's commit now was captured on top of it, so the person's
// yes for that commit covers it, and the person's own commits above. One
// that doesn't was captured before the branch was refreshed to another
// author's commit, whose yes says nothing of the code the revision holds,
// which the newer push may have taken back (Codex's confirmation of
// #162's fixes, finding 1): it is refused.
func (e *Engine) evaluatingRevision(ctx context.Context, revision model.Revision) (context.Context, error) {
	branch, err := e.Branch(ctx, revision.Branch)
	if err != nil {
		return ctx, err
	}
	marked, refused := e.evaluating(ctx, branch)
	whose, others := OthersPortfiles(branch)
	if refused != nil || !others {
		return marked, refused
	}
	head := authorHead(branch.PullRequest)
	at := revision.Source.Commit
	if revision.Kind == model.RevisionSnapshot {
		at = revision.Head
	}
	if at != "" {
		if holds, err := e.Repo.IsAncestor(ctx, string(head), string(at)); err == nil && holds {
			return marked, nil
		}
	}
	number := branch.PullRequest.Number
	refused = refusal.New(refusal.Failed, "these files of #%d, %s, were captured before its author's commit %s, the one you allowed, so the author's commit behind them isn't one you allowed; dockhand check checks the branch as it is now", number, whose, head.Short()).
		Then(refusal.Next{Verb: refusal.VerbCheck, Args: []string{"--branch", branch.ShortName()}})
	return workspace.WithOwner(ctx, workspace.Owner{Whose: whose, Of: branch.ShortName(), Refused: refused}), refused
}

// AdoptedDiff is what an adopted branch's author's commit changes from
// its base, of the code MacPorts evaluates: its Portfiles and PortGroups.
func (e *Engine) AdoptedDiff(ctx context.Context, branch model.Branch) ([]byte, error) {
	pr := branch.PullRequest
	if pr == nil {
		return nil, nil
	}
	head := string(authorHead(pr))
	trees, err := e.Repo.CommitTrees(ctx, []string{string(branch.Base), head})
	if err != nil {
		return nil, err
	}
	changed, err := e.Repo.ChangedPaths(ctx, trees[string(branch.Base)], trees[head])
	if err != nil {
		return nil, err
	}
	return e.Repo.DiffTrees(ctx, trees[string(branch.Base)], trees[head], code(changed)...)
}
