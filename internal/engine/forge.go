package engine

import (
	"context"

	"github.com/herbygillot/dockhand/internal/forge"
)

// Forge is what submit needs of GitHub: who you are, which repositories
// your remotes name, the pull requests, and dockhand's own repository.
type Forge interface {
	AuthenticatedUser(ctx context.Context) (string, error)
	NameFromRemote(url string) (string, error)
	RepositoryInfo(ctx context.Context, name string) (forge.RepositoryInfo, error)
	Find(ctx context.Context, query forge.PullRequestQuery) (forge.PullRequestObservation, error)
	Observe(ctx context.Context, ref forge.PullRequestRef) (forge.PullRequestObservation, error)
	Create(ctx context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error)
	Update(ctx context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error)
	OpenPullRequests(ctx context.Context, repository, port string) ([]forge.PullRequestSummary, error)
	// Inspect reports a pull request's reviews and checks.
	Inspect(ctx context.Context, ref forge.PullRequestRef) (forge.PullRequestStatus, error)
	// MarkReady takes a draft out of draft.
	MarkReady(ctx context.Context, ref forge.PullRequestRef) (forge.PullRequestObservation, error)
	// Permission is a person's role on a repository.
	Permission(ctx context.Context, repository, login string) (string, error)
	// PostReview posts a review on a pull request.
	PostReview(ctx context.Context, input forge.ReviewInput) (string, error)
	// RequestReviewers asks people to review a pull request again.
	RequestReviewers(ctx context.Context, ref forge.PullRequestRef, logins []string) error
	// Repository binds a repository for reading, such as dockhand's own,
	// asked whether it has the builds a branch's commits name.
	Repository(instance, name string) (forge.Repository, error)
}

// GitHubCLI is the GitHub CLI, which acts on GitHub as its own app. An
// organization that refuses dockhand's app may take it, as the macports
// organization does to mark a draft ready (D8).
type GitHubCLI interface {
	// Login is the account it's signed in as.
	Login(ctx context.Context) (string, error)
	// MarkReady takes a draft pull request out of draft.
	MarkReady(ctx context.Context, ref forge.PullRequestRef) error
}

// forge is the engine's Forge: the one it was given, or GitHub with the
// login from the system keychain (or GH_TOKEN), assembled on first use.
func (e *Engine) forge() Forge {
	e.lazy.Lock()
	defer e.lazy.Unlock()
	if e.Forge == nil {
		e.Forge, e.forgeMade = e.github(), true
	}
	return e.Forge
}

// dropForge lets go of the GitHub client the engine assembled, and the
// token it holds, so the next use reads the login as it is now: serve's,
// when the login changed under it. One the engine was given stays.
func (e *Engine) dropForge() {
	e.lazy.Lock()
	defer e.lazy.Unlock()
	if e.forgeMade {
		e.Forge, e.forgeMade = nil, false
	}
}

// pullRequestRef is a pull request on GitHub, where MacPorts' pull
// requests are, by its base repository and number.
func pullRequestRef(repository string, number int) forge.PullRequestRef {
	return forge.PullRequestRef{Forge: forge.GitHub, Repository: repository, Number: number}
}
