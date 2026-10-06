// Package forgetest is GitHub as the engine's and the command line's tests
// stand it in: one stateful fake, its fork a local bare repository and its
// pull requests in memory, so each package's tests drive the same
// behavior rather than a fake of their own (the test plan's step 1,
// 2026-10-02). It checks no calls; tests read its state.
package forgetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/model"
)

// UpstreamRepository is MacPorts' ports repository, which pull requests
// are opened against.
const UpstreamRepository = "macports/macports-ports"

// ForkRepository is the person's fork, ada's.
const ForkRepository = "ada/macports-ports"

// GitHub is GitHub as a test sets it up.
type GitHub struct {
	// Upstream and Fork are the local repositories MacPorts' and the
	// person's fork stand for.
	Upstream, Fork string
	// ForkParent is the fork's parent as GitHub reports it; MacPorts'
	// where empty. Another names a fork of something else.
	ForkParent string
	// Repos are other people's repositories, by name, as local paths.
	Repos map[string]string
	// PRs are the pull requests opened, by number, from 34901; Drafts
	// whether each was opened as a draft, in order.
	PRs    map[int]*forge.PullRequest
	Drafts []bool
	next   int
	// Theirs are other people's pull requests, by number, as GitHub
	// reports them.
	Theirs map[int]forge.PullRequest
	// Created and Updated are the inputs Create and Update were given.
	Created, Updated []forge.PullRequestInput
	// CreateFails fails the next Create: after opening the pull request,
	// as a reply lost on the way back, when Lost is set, or before.
	CreateFails, Lost bool
	// Others are the open pull requests a search for a port finds, and
	// SearchErr why it fails. The first Quiet searches find nothing, as
	// before another was opened; Searches counts them all.
	Others    []forge.PullRequestSummary
	SearchErr error
	Quiet     int
	Searches  int
	// Readied are the pull requests marked ready; ReadyRefused is GitHub
	// refusing to mark one.
	Readied      []int
	ReadyRefused error
	// Role is what Permission reports; read where empty.
	Role string
	// Reviews are those posted, and Rerequested the reviewers asked again.
	Reviews     []forge.ReviewInput
	Rerequested []string
	// Statuses are what Inspect reports, by number; Status for any other,
	// and no review where it's empty.
	Statuses map[int]forge.PullRequestStatus
	Status   forge.PullRequestStatus
	// Dockhand is dockhand's own repository as GitHub has it.
	Dockhand Dockhand
}

// New is GitHub with MacPorts at upstream and the person's fork at fork.
func New(upstream, fork string) *GitHub {
	return &GitHub{Upstream: upstream, Fork: fork, PRs: map[int]*forge.PullRequest{}}
}

// Dockhand is dockhand's own repository on GitHub: its commits and tags,
// why asking it fails, and what was asked of it.
type Dockhand struct {
	name    string
	Commits []string
	Tags    []string
	Err     error
	Asked   []string
}

func (d *Dockhand) Name() string { return d.name }

func (d *Dockhand) Tag(_ context.Context, name string) (forge.Tag, error) {
	d.Asked = append(d.Asked, name)
	switch {
	case d.Err != nil:
		return forge.Tag{}, d.Err
	case slices.Contains(d.Tags, name):
		return forge.Tag{Name: name, Commit: strings.Repeat("d", 40)}, nil
	}
	return forge.Tag{}, forge.ErrNotFound
}

func (d *Dockhand) ListTags(context.Context) ([]forge.Tag, error) { return nil, nil }

func (d *Dockhand) HasCommit(_ context.Context, commit string) (bool, error) {
	d.Asked = append(d.Asked, commit)
	if d.Err != nil {
		return false, d.Err
	}
	return slices.ContainsFunc(d.Commits, func(c string) bool { return strings.HasPrefix(c, commit) }), nil
}

func (g *GitHub) Repository(_, name string) (forge.Repository, error) {
	g.Dockhand.name = name
	return &g.Dockhand, nil
}

func (g *GitHub) AuthenticatedUser(context.Context) (string, error) { return "ada", nil }

func (g *GitHub) NameFromRemote(url string) (string, error) {
	for name, path := range g.Repos {
		if path == url {
			return name, nil
		}
	}
	switch url {
	case g.Upstream:
		return UpstreamRepository, nil
	case g.Fork:
		return ForkRepository, nil
	}
	return "", errors.New("not a GitHub remote")
}

func (g *GitHub) RepositoryInfo(_ context.Context, name string) (forge.RepositoryInfo, error) {
	if name == ForkRepository {
		parent := g.ForkParent
		if parent == "" {
			parent = UpstreamRepository
		}
		return forge.RepositoryInfo{Name: name, DefaultBranch: "master", Parent: parent}, nil
	}
	return forge.RepositoryInfo{Name: name, DefaultBranch: "master"}, nil
}

// head is the commit a repository's branch is at, the fork's where repo
// is none of Repos; empty where it has none.
func (g *GitHub) head(repo, branch string) model.ObjectID {
	path := g.Fork
	if other, ok := g.Repos[repo]; ok {
		path = other
	}
	if path == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", path, "for-each-ref", "--format=%(objectname)", "refs/heads/"+branch).Output()
	if err != nil {
		return ""
	}
	return model.ObjectID(bytes.TrimSpace(out))
}

// observe is a pull request as GitHub reports it: at the head its branch
// is at now, whoever pushed it.
func (g *GitHub) observe(pr *forge.PullRequest) forge.PullRequestObservation {
	copied := *pr
	if head := g.head(pr.HeadRepository, pr.HeadBranch); head != "" {
		copied.RemoteHead = head
	}
	return forge.PullRequestObservation{Found: true, PullRequest: copied}
}

func (g *GitHub) Find(_ context.Context, q forge.PullRequestQuery) (forge.PullRequestObservation, error) {
	// As GitHub's: the open one, else the latest.
	var found *forge.PullRequest
	for _, number := range slices.Sorted(maps.Keys(g.PRs)) {
		pr := g.PRs[number]
		if pr.HeadRepository == q.HeadRepository && pr.HeadBranch == q.HeadBranch && (found == nil || found.State != forge.PullRequestOpen) {
			found = pr
		}
	}
	if found == nil {
		return forge.PullRequestObservation{}, nil
	}
	return g.observe(found), nil
}

func (g *GitHub) Observe(_ context.Context, ref forge.PullRequestRef) (forge.PullRequestObservation, error) {
	if pr, ok := g.Theirs[ref.Number]; ok {
		return forge.PullRequestObservation{Found: true, PullRequest: pr}, nil
	}
	pr, ok := g.PRs[ref.Number]
	if !ok {
		return forge.PullRequestObservation{}, forge.ErrNotFound
	}
	return g.observe(pr), nil
}

func (g *GitHub) Create(_ context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	if g.CreateFails && !g.Lost {
		g.CreateFails = false
		return forge.PullRequestObservation{}, errors.New("github: connection reset before the request was sent")
	}
	// GitHub refuses a second pull request from one head to one base.
	for _, pr := range g.PRs {
		if pr.HeadRepository == input.HeadRepository && pr.HeadBranch == input.HeadBranch && pr.BaseBranch == input.BaseBranch && pr.State == forge.PullRequestOpen {
			return forge.PullRequestObservation{}, fmt.Errorf("%w: a pull request already exists for %s:%s", forge.ErrRejected, input.HeadRepository, input.HeadBranch)
		}
	}
	if g.PRs == nil {
		g.PRs = map[int]*forge.PullRequest{}
	}
	g.Created = append(g.Created, input)
	g.Drafts = append(g.Drafts, input.Draft)
	g.next++
	number := 34900 + g.next
	g.PRs[number] = &forge.PullRequest{Ref: forge.PullRequestRef{Forge: forge.GitHub, Repository: input.Repository, Number: number, URL: fmt.Sprintf("https://github.com/%s/pull/%d", input.Repository, number)},
		HeadRepository: input.HeadRepository, HeadBranch: input.HeadBranch, BaseBranch: input.BaseBranch, State: forge.PullRequestOpen, Title: input.Desired.Title, Body: input.Desired.Body, RemoteHead: input.Desired.Head}
	if g.CreateFails {
		g.CreateFails = false
		return forge.PullRequestObservation{}, errors.New("github: connection reset after the request was sent")
	}
	return g.observe(g.PRs[number]), nil
}

func (g *GitHub) Update(_ context.Context, input forge.PullRequestInput) (forge.PullRequestObservation, error) {
	g.Updated = append(g.Updated, input)
	pr, ok := g.PRs[input.ExistingPR.Number]
	if !ok {
		return forge.PullRequestObservation{}, forge.ErrNotFound
	}
	pr.Title, pr.Body = input.Desired.Title, input.Desired.Body
	if input.Desired.Head != "" {
		pr.RemoteHead = input.Desired.Head
	}
	return g.observe(pr), nil
}

func (g *GitHub) OpenPullRequests(context.Context, string, string) ([]forge.PullRequestSummary, error) {
	g.Searches++
	if g.Searches <= g.Quiet {
		return nil, nil
	}
	return g.Others, g.SearchErr
}

func (g *GitHub) MarkReady(ctx context.Context, ref forge.PullRequestRef) (forge.PullRequestObservation, error) {
	if g.ReadyRefused != nil {
		return forge.PullRequestObservation{}, g.ReadyRefused
	}
	g.Readied = append(g.Readied, ref.Number)
	return g.Observe(ctx, ref)
}

func (g *GitHub) Permission(context.Context, string, string) (string, error) {
	if g.Role == "" {
		return "read", nil
	}
	return g.Role, nil
}

func (g *GitHub) PostReview(_ context.Context, input forge.ReviewInput) (string, error) {
	g.Reviews = append(g.Reviews, input)
	return fmt.Sprintf("https://github.com/%s/pull/%d#pullrequestreview-%d", input.Ref.Repository, input.Ref.Number, len(g.Reviews)), nil
}

func (g *GitHub) RequestReviewers(_ context.Context, _ forge.PullRequestRef, logins []string) error {
	g.Rerequested = append(g.Rerequested, logins...)
	return nil
}

func (g *GitHub) Inspect(_ context.Context, ref forge.PullRequestRef) (forge.PullRequestStatus, error) {
	if status, ok := g.Statuses[ref.Number]; ok {
		return status, nil
	}
	if g.Status.Review != "" {
		return g.Status, nil
	}
	status := g.Status
	status.Review = "none"
	return status, nil
}

// ForkHead is the commit the fork's branch is at; empty where the fork
// has no such branch.
func (g *GitHub) ForkHead(branch string) model.ObjectID { return g.head(ForkRepository, branch) }
