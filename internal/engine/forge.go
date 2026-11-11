package engine

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/remotewrite"
)

// Forge is what submit needs of GitHub: who you are, which repositories
// your remotes name, the pull requests, and dockhand's own repository.
type Forge interface {
	AuthenticatedUser(ctx context.Context) (string, error)
	// Login is who you are, and where the token comes from.
	Login(ctx context.Context) (forge.Login, error)
	NameFromRemote(url string) (string, error)
	RepositoryInfo(ctx context.Context, name string) (forge.RepositoryInfo, error)
	Find(ctx context.Context, query forge.PullRequestQuery) (forge.PullRequestObservation, error)
	Observe(ctx context.Context, ref forge.PullRequestRef) (forge.PullRequestObservation, error)
	OpenPullRequests(ctx context.Context, repository, port string) ([]forge.PullRequestSummary, error)
	// Inspect reports a pull request's reviews and checks.
	Inspect(ctx context.Context, ref forge.PullRequestRef) (forge.PullRequestStatus, error)
	// Permission is a person's role on a repository.
	Permission(ctx context.Context, repository, login string) (string, error)
	// Repository binds a repository for reading, such as dockhand's own,
	// asked whether it has the builds a branch's commits name.
	Repository(instance, name string) (forge.Repository, error)
	// Describe reads a repository's description, under the name it
	// answers by now, which a renamed one's differs from the name asked:
	// what an update asks of every release's repository, where it found
	// by type assertion whether a repository could say (C3).
	Describe(ctx context.Context, instance, name string) (forge.Description, error)
	// Account reads a login as the forge has it now, for a repository
	// move's old owner name.
	Account(ctx context.Context, instance, login string) (forge.Account, error)
	// FailedJobs are a head's CI jobs that failed, of each run's latest
	// attempt, and JobLog a job's log, forge.ErrNotFound where the forge no
	// longer keeps it or won't give it. Its writes, a pull request's
	// creation and edits, reviews, and CI run again, are remotewrite's alone
	// (remotewrite.PullRequests, #128).
	FailedJobs(ctx context.Context, repository, head string) ([]forge.CIJob, error)
	JobLog(ctx context.Context, repository string, job int64) ([]byte, error)
}

// GitHubCLI is the GitHub CLI, which acts on GitHub as its own app. An
// organization that refuses dockhand's app may take it, as the macports
// organization does to mark a draft ready (D8).
type GitHubCLI interface {
	// Login is the account it's signed in as.
	Login(ctx context.Context) (string, error)
	// MarkReady takes a draft pull request out of draft, and MarkDraft
	// returns one to draft.
	MarkReady(ctx context.Context, ref forge.PullRequestRef) error
	MarkDraft(ctx context.Context, ref forge.PullRequestRef) error
}

// pullRequests are the engine's Forge's writes, which only remotewrite calls
// (#128).
func (e *Engine) pullRequests() remotewrite.PullRequests {
	if writes, ok := e.forge().(remotewrite.PullRequests); ok {
		return writes
	}
	return readOnly{}
}

// readOnly is a forge that writes nothing, saying so.
type readOnly struct{}

func (readOnly) refuse() error {
	return errors.New("the configured forge does not support remote writes")
}
func (r readOnly) Create(context.Context, forge.PullRequestInput) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, r.refuse()
}
func (r readOnly) Update(context.Context, forge.PullRequestInput) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, r.refuse()
}
func (r readOnly) MarkReady(context.Context, forge.PullRequestRef) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, r.refuse()
}
func (r readOnly) MarkDraft(context.Context, forge.PullRequestRef) (forge.PullRequestObservation, error) {
	return forge.PullRequestObservation{}, r.refuse()
}
func (r readOnly) PostReview(context.Context, forge.ReviewInput) (string, error) {
	return "", r.refuse()
}
func (r readOnly) RequestReviewers(context.Context, forge.PullRequestRef, []string) error {
	return r.refuse()
}
func (r readOnly) RerunFailed(context.Context, string, int64) error { return r.refuse() }

// forge is the engine's Forge: the one it was given, or GitHub with the
// login compose gave the runtime, assembled on first use.
func (e *Engine) forge() Forge {
	e.lazy.Lock()
	defer e.lazy.Unlock()
	if e.Forge == nil {
		e.Forge, e.forgeMade = e.github(), true
	}
	return e.Forge
}

// dropForge lets go of the login the engine's GitHub runtime holds, so the
// next use reads it as it is now: serve's, when the login changed under
// it. A forge the engine was given stays as it is.
func (e *Engine) dropForge() {
	e.lazy.Lock()
	defer e.lazy.Unlock()
	if e.forgeMade {
		// The runtime the forge shares with discovery and the rest forgets
		// the login, in one place for all of them.
		e.GitHub.AccountChanged()
	}
}

// pullRequestRef is a pull request on GitHub, where MacPorts' pull
// requests are, by its base repository and number.
func pullRequestRef(repository string, number int) forge.PullRequestRef {
	return forge.PullRequestRef{Forge: forge.GitHub, Repository: repository, Number: number}
}
