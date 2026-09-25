package engine

import (
	"context"
	"net/http"

	"github.com/herbygillot/dockhand/internal/credential/keychain"
	"github.com/herbygillot/dockhand/internal/forge"
	forgegithub "github.com/herbygillot/dockhand/internal/forge/github"
	"github.com/herbygillot/dockhand/internal/github"
	"github.com/herbygillot/dockhand/internal/record"
)

// Forge is what submit needs of GitHub: who you are, which repositories
// your remotes name, and the pull requests.
type Forge interface {
	AuthenticatedUser(ctx context.Context) (string, error)
	NameFromRemote(url string) (string, error)
	RepositoryInfo(ctx context.Context, name string) (forge.RepositoryInfo, error)
	Find(ctx context.Context, query forge.PullRequestQuery) (forge.PullRequestObservation, error)
	Observe(ctx context.Context, ref record.PullRequestRef) (forge.PullRequestObservation, error)
	Create(ctx context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error)
	Update(ctx context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error)
	OpenPullRequests(ctx context.Context, repository, port string) ([]forge.PullRequestSummary, error)
	// Inspect reports a pull request's reviews and checks.
	Inspect(ctx context.Context, ref record.PullRequestRef) (record.PullRequestStatus, error)
	// MarkReady takes a draft out of draft.
	MarkReady(ctx context.Context, ref record.PullRequestRef) (forge.PullRequestObservation, error)
	// Permission is a person's role on a repository.
	Permission(ctx context.Context, repository, login string) (string, error)
	// PostReview posts a review on a pull request.
	PostReview(ctx context.Context, input forge.ReviewInput) (string, error)
	// RequestReviewers asks people to review a pull request again.
	RequestReviewers(ctx context.Context, ref record.PullRequestRef, logins []string) error
}

// forge is the engine's Forge: the one it was given, or GitHub with the
// login from the system keychain (or GH_TOKEN), assembled on first use.
func (e *Engine) forge() Forge {
	if e.Forge == nil {
		client := &github.Client{HTTP: http.DefaultClient, Credentials: github.SystemCredentials{Store: keychain.Store{}, Key: github.CredentialKey}}
		e.Forge = &forgegithub.Client{Client: client, GitExecutable: e.options.Git}
	}
	return e.Forge
}
