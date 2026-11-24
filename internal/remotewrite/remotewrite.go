// Package remotewrite is dockhand's only caller of what changes something
// remote: a branch pushed or deleted, a pull request opened, edited,
// returned to draft or marked ready, reviewers asked, a review posted, a
// pull request's checks run again, and a workflow run canceled or run
// again. Each takes the repository it changes and the basis dockhand has
// for changing it, and one table of effect against basis decides
// (Permit), with two absolutes: branch pushes/deletions and changes to
// workflow runs managed by checks never target MacPorts' repository, and a
// deletion needs a record that dockhand made what it deletes. Explicit
// pull-request CI retries use RerunChecks, which is permitted there.
// "dockhand changes only what is yours or what it made" was written seven
// ways in seven places, and #112 deleted an adopted pull request's branch
// in a repository that wasn't the person's (#128; docs/principles.md).
package remotewrite

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/refusal"
)

type IdentityReader interface {
	Current(context.Context) (github.Identity, error)
}

// RemoteNamer names a remote through the forge that owns its address format.
type RemoteNamer interface {
	NameFromRemote(string) (string, error)
}

type accountBinding struct {
	account  string
	identity IdentityReader
}
type accountBindingKey struct{}
type remoteNamerKey struct{}

// ForAccount binds subsequent writes to the account named in a preview (#250).
func ForAccount(ctx context.Context, account string, identity IdentityReader) context.Context {
	return context.WithValue(ctx, accountBindingKey{}, accountBinding{account: account, identity: identity})
}

// ForRemote binds remote naming to the forge responsible for the remote URL.
func ForRemote(ctx context.Context, namer RemoteNamer) context.Context {
	return context.WithValue(ctx, remoteNamerKey{}, namer)
}

func write(ctx context.Context, run func(context.Context) error) error {
	binding, ok := ctx.Value(accountBindingKey{}).(accountBinding)
	if !ok || binding.identity == nil || strings.TrimSpace(binding.account) == "" {
		return errors.New("remote write requires an account binding")
	}
	check := func() error {
		current, err := binding.identity.Current(ctx)
		if err != nil {
			return fmt.Errorf("reading the GitHub account from the preview: %w", err)
		}
		if !strings.EqualFold(current.Account, binding.account) {
			return errors.New("the account changed; run it again")
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	writeCtx := github.AsWrite(ctx)
	err := run(writeCtx)
	if !errors.Is(err, github.ErrCredentialRenewed) {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	return run(writeCtx)
}

// Upstream is MacPorts' repository, where pull requests are opened and
// nothing dockhand pushes lands.
const Upstream = "macports/macports-ports"

// Basis is why dockhand may change a remote thing.
type Basis string

const (
	// OwnFork is the person's fork of MacPorts' repository, as dockhand
	// resolved it: theirs to push to.
	OwnFork Basis = "own-fork"
	// Made is a record that dockhand made the thing: a branch's pull
	// request it opened and pushed, not one adopted; a check branch with
	// its prefix and recorded commit; a workflow run an execution
	// recorded.
	Made Basis = "made"
	// Invited is someone's pull request whose branch GitHub lets the
	// person push to: maintainer edits on, with write access.
	Invited Basis = "invited"
	// Named is the person naming this exact action on the command line,
	// as review <pr> does.
	Named Basis = "named"
)

// Effect is a kind of remote change.
type Effect string

const (
	PushBranch       Effect = "push a branch"
	DeleteBranch     Effect = "delete a branch"
	OpenPullRequest  Effect = "open a pull request"
	EditPullRequest  Effect = "edit a pull request"
	DraftPullRequest Effect = "return a pull request to draft"
	ReadyPullRequest Effect = "mark a pull request ready"
	AskReviewers     Effect = "ask reviewers again"
	PostReview       Effect = "post a review"
	RerunChecks      Effect = "run a pull request's checks again"
	CancelRun        Effect = "cancel a workflow run"
	RerunRun         Effect = "run a workflow run again"
)

// The rule, in the form an observer outside dockhand can check, which
// the acceptance harness's H2 and H3 and the fault sweep's ownership
// invariant check against, each citing its line, without reading this
// table: a check that asked the table couldn't catch it being wrong
// (#144).
//
//  1. In MacPorts' repository no branch or tag changes and no workflow
//     run is canceled or run again. Pull requests are opened and edited
//     there.
//  2. In another person's repository nothing changes but a push to the
//     head branch of a pull request they opened with maintainer edits
//     on. Nothing is deleted there.
//  3. A branch is deleted only where a record says dockhand made it.
//  4. A pull request someone else opened is edited, marked or reviewed
//     only where the person was invited or named that action.
//
// rules are the bases each effect may act on.
var rules = map[Effect][]Basis{
	PushBranch:       {OwnFork, Made, Invited},
	DeleteBranch:     {Made},
	OpenPullRequest:  {OwnFork, Made},
	EditPullRequest:  {Made, Invited, Named},
	DraftPullRequest: {Made, Invited, Named},
	ReadyPullRequest: {Made, Invited, Named},
	AskReviewers:     {Made, Named},
	PostReview:       {Named},
	RerunChecks:      {Made, Named},
	CancelRun:        {OwnFork, Made},
	RerunRun:         {OwnFork, Made},
}

// inUpstream are the effects that never land in MacPorts' repository:
// branch pushes/deletions and managed-workflow cancellations/reruns.
// Pull requests live there; their writes and explicit CI retries are
// judged by their own effect's rule.
var inUpstream = []Effect{PushBranch, DeleteBranch, CancelRun, RerunRun}

// Permit says whether dockhand may have an effect on a repository on a
// basis, a refusal saying which rule where it may not.
func Permit(effect Effect, repository string, basis Basis) error {
	if slices.Contains(inUpstream, effect) && strings.EqualFold(repository, Upstream) {
		return refusal.New(refusal.Unsupported, "dockhand doesn't %s in %s: this operation is never allowed in the upstream repository, regardless of ownership", effect, Upstream)
	}
	if !slices.Contains(rules[effect], basis) {
		return refusal.New(refusal.Unsupported, "dockhand doesn't %s in %s based only on %s: that is not sufficient authorization for this operation", effect, repository, words[basis])
	}
	return nil
}

func permitRemote(ctx context.Context, effect Effect, repository, remote string, basis Basis) error {
	namer, ok := ctx.Value(remoteNamerKey{}).(RemoteNamer)
	if !ok || namer == nil {
		return refusal.New(refusal.Unsupported, "dockhand doesn't %s to an unnamed remote: no forge remote namer is bound", effect)
	}
	remoteRepository, err := namer.NameFromRemote(remote)
	if err != nil || strings.TrimSpace(remoteRepository) == "" {
		if err == nil {
			err = errors.New("forge returned an empty repository name")
		}
		return refusal.New(refusal.Unsupported, "dockhand doesn't %s to an unnamed remote: %s", effect, err)
	}
	if !strings.EqualFold(repository, remoteRepository) {
		return refusal.New(refusal.Unsupported, "remote write to %s does not match permitted repository %s", remoteRepository, repository)
	}
	return Permit(effect, repository, basis)
}

// words say a basis as a refusal names it.
var words = map[Basis]string{OwnFork: "your fork", Made: "a record that it made it", Invited: "an invitation to push", Named: "your naming it"}

// Push pushes a branch to a repository (git.Repository.Push), and says
// what it did: a push refused here sends nothing.
func Push(ctx context.Context, repo *git.Repository, basis Basis, repository string, push git.Push) (git.PushReport, error) {
	if err := permitRemote(ctx, PushBranch, repository, push.Remote, basis); err != nil {
		return git.PushReport{}, err
	}
	var report git.PushReport
	err := write(ctx, func(ctx context.Context) error {
		var err error
		report, err = repo.Push(ctx, push)
		return err
	})
	return report, err
}

// Delete deletes a repository's branch where it still holds expected
// (git.Repository.DeleteRemoteBranch).
func Delete(ctx context.Context, repo *git.Repository, basis Basis, repository, remote, branch string, expected git.RefValue) error {
	if err := permitRemote(ctx, DeleteBranch, repository, remote, basis); err != nil {
		return err
	}
	return write(ctx, func(ctx context.Context) error {
		return repo.DeleteRemoteBranch(ctx, remote, branch, expected)
	})
}

// PullRequests are a forge's writes to pull requests and their checks.
type PullRequests interface {
	Create(ctx context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error)
	Update(ctx context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error)
	MarkReady(ctx context.Context, ref forge.PullRequestRef) (forge.PullRequestObservation, error)
	MarkDraft(ctx context.Context, ref forge.PullRequestRef) (forge.PullRequestObservation, error)
	PostReview(ctx context.Context, input forge.ReviewInput) (string, error)
	RequestReviewers(ctx context.Context, ref forge.PullRequestRef, logins []string) error
	RerunFailed(ctx context.Context, repository string, run int64) error
}

// Open opens a pull request.
func Open(ctx context.Context, f PullRequests, basis Basis, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	if err := Permit(OpenPullRequest, input.Repository, basis); err != nil {
		return forge.PullRequestObservation{}, err
	}
	var result forge.PullRequestObservation
	err := write(ctx, func(ctx context.Context) error { var err error; result, err = f.Create(ctx, input); return err })
	return result, err
}

// Edit edits an open pull request's title and description.
func Edit(ctx context.Context, f PullRequests, basis Basis, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	if err := Permit(EditPullRequest, input.Repository, basis); err != nil {
		return forge.PullRequestObservation{}, err
	}
	var result forge.PullRequestObservation
	err := write(ctx, func(ctx context.Context) error { var err error; result, err = f.Update(ctx, input); return err })
	return result, err
}

// Draft returns a pull request to draft.
func Draft(ctx context.Context, f PullRequests, basis Basis, ref forge.PullRequestRef) (forge.PullRequestObservation, error) {
	if err := Permit(DraftPullRequest, ref.Repository, basis); err != nil {
		return forge.PullRequestObservation{}, err
	}
	var result forge.PullRequestObservation
	err := write(ctx, func(ctx context.Context) error { var err error; result, err = f.MarkDraft(ctx, ref); return err })
	return result, err
}

// Ready takes a pull request out of draft.
func Ready(ctx context.Context, f PullRequests, basis Basis, ref forge.PullRequestRef) (forge.PullRequestObservation, error) {
	if err := Permit(ReadyPullRequest, ref.Repository, basis); err != nil {
		return forge.PullRequestObservation{}, err
	}
	var result forge.PullRequestObservation
	err := write(ctx, func(ctx context.Context) error { var err error; result, err = f.MarkReady(ctx, ref); return err })
	return result, err
}

// Ask asks people to review a pull request again.
func Ask(ctx context.Context, f PullRequests, basis Basis, ref forge.PullRequestRef, logins []string) error {
	if err := Permit(AskReviewers, ref.Repository, basis); err != nil {
		return err
	}
	return write(ctx, func(ctx context.Context) error { return f.RequestReviewers(ctx, ref, logins) })
}

// Review posts a review on a pull request.
func Review(ctx context.Context, f PullRequests, basis Basis, input forge.ReviewInput) (string, error) {
	if err := Permit(PostReview, input.Ref.Repository, basis); err != nil {
		return "", err
	}
	var result string
	err := write(ctx, func(ctx context.Context) error { var err error; result, err = f.PostReview(ctx, input); return err })
	return result, err
}

// RerunFailed runs a pull request's failed CI jobs again.
func RerunFailed(ctx context.Context, f PullRequests, basis Basis, repository string, run int64) error {
	if err := Permit(RerunChecks, repository, basis); err != nil {
		return err
	}
	return write(ctx, func(ctx context.Context) error { return f.RerunFailed(ctx, repository, run) })
}

// CLI is the GitHub CLI's writes, which act as its own app where an
// organization refuses dockhand's.
type CLI interface {
	MarkReady(ctx context.Context, ref forge.PullRequestRef) error
	MarkDraft(ctx context.Context, ref forge.PullRequestRef) error
}

// DraftByCLI returns a pull request to draft through the GitHub CLI.
func DraftByCLI(ctx context.Context, cli CLI, basis Basis, ref forge.PullRequestRef) error {
	if err := Permit(DraftPullRequest, ref.Repository, basis); err != nil {
		return err
	}
	return write(ctx, func(ctx context.Context) error { return cli.MarkDraft(ctx, ref) })
}

// ReadyByCLI takes a pull request out of draft through the GitHub CLI.
func ReadyByCLI(ctx context.Context, cli CLI, basis Basis, ref forge.PullRequestRef) error {
	if err := Permit(ReadyPullRequest, ref.Repository, basis); err != nil {
		return err
	}
	return write(ctx, func(ctx context.Context) error { return cli.MarkReady(ctx, ref) })
}

// Runs are GitHub Actions' writes to a workflow run.
type Runs interface {
	Cancel(ctx context.Context, repository string, id int64) error
	Rerun(ctx context.Context, repository string, id int64) error
}

// Cancel cancels a workflow run that hasn't finished.
func Cancel(ctx context.Context, runs Runs, basis Basis, repository string, id int64) error {
	if err := Permit(CancelRun, repository, basis); err != nil {
		return err
	}
	return write(ctx, func(ctx context.Context) error { return runs.Cancel(ctx, repository, id) })
}

// Rerun runs a workflow run's unsuccessful jobs again.
func Rerun(ctx context.Context, runs Runs, basis Basis, repository string, id int64) error {
	if err := Permit(RerunRun, repository, basis); err != nil {
		return err
	}
	return write(ctx, func(ctx context.Context) error { return runs.Rerun(ctx, repository, id) })
}
