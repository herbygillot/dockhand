// Package gh is everything dockhand asks of GitHub: the gh seam,
// pull-request lookup and search, and fork and upstream resolution. It
// sits above git (a remote URL and a branch's tracking config are where
// every lookup starts) and below cmd, which orchestrates it.
//
// It answers with facts and keeps no opinion. What a pull request MEANS
// is mapped by the caller into the facts verdict weighs, and how any of
// it is worded belongs to render, so no judgment has to know gh's
// spelling and this package holds none of its own.
package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/tool"
)

// OpenPortPRs lists the open upstream PRs whose titles claim the same
// port, leaning on the project convention that a title is
// `<port>: <description>` — dockhand's own titles included. The search
// API bounds and ranks the result; the prefix filter runs here because
// in:title matches the term anywhere in a title.
func OpenPortPRs(ctx context.Context, gh Runner, upstream, port string) ([]PullRequest, error) {
	if port == "" {
		return nil, nil
	}
	out, err := gh(ctx, "api", "-X", "GET", "search/issues",
		"-f", fmt.Sprintf("q=repo:%s is:pr is:open in:title %q", upstream, port+":"))
	if err != nil {
		return nil, err
	}
	var res struct {
		Items []PullRequest `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("reading PR search: %w", err)
	}
	var prs []PullRequest
	for _, pr := range res.Items {
		if strings.HasPrefix(pr.Title, port+":") {
			prs = append(prs, pr)
		}
	}
	return prs, nil
}

// UpstreamRepo names the owner/repo the PR targets: the remote the
// primary branch tracks — where the work forked from is where it goes
// back to.
func UpstreamRepo(ctx context.Context, repo *git.Repo) (string, error) {
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return "", err
	}
	remote := repo.TrackedRemote(ctx, primary)
	if remote == "" {
		remote = "origin"
	}
	remotes, err := repo.Remotes(ctx)
	if err != nil {
		return "", err
	}
	owner, name, ok := OwnerRepoFromURL(remotes[remote])
	if !ok {
		return "", fmt.Errorf("cannot read owner/repo from remote %q (%s)", remote, remotes[remote])
	}
	return owner + "/" + name, nil
}

// OwnerRepoFromURL reads owner and repository out of a git remote URL,
// in both the ssh (git@host:owner/repo.git) and https
// (https://host/owner/repo) spellings.
//
// It is host-agnostic and takes the last two path segments, because
// what it is asked about is a remote of the checkout in hand. It is
// deliberately not the parser upstream's release check uses: that one
// is github.com-only by design, and its refusal is the signal that
// sends a non-GitHub project to its tags instead. Merging the two would
// point the releases API at repositories that are not on GitHub.
func OwnerRepoFromURL(url string) (owner, repo string, ok bool) {
	s := strings.TrimSuffix(url, ".git")
	if _, rest, found := strings.Cut(s, ":"); found && !strings.Contains(s, "://") {
		s = rest
	} else if _, rest, found := strings.Cut(s, "://"); found {
		if _, path, found := strings.Cut(rest, "/"); found {
			s = path
		}
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[len(parts)-2], parts[len(parts)-1], true
}

// Runner is the gh seam's shape: one invocation, stdout back. The
// composition root wires RealGhOut's runner into runstate.Context;
// every function here takes a Runner rather than reaching for a
// package variable, which is what lets a test hand in a scripted
// GitHub without mutating globals.
type Runner func(ctx context.Context, args ...string) (string, error)

// RealGhOut is the runner over the actual gh CLI, resolved through the
// run's finder. A miss names the remedy; a failed call reads
// "gh <subcommand>: <stderr>", with the exec error standing in for a
// stderr gh left empty.
func RealGhOut(tools *tool.Finder) Runner {
	return func(ctx context.Context, args ...string) (string, error) {
		bin, err := tools.Find(tool.Gh)
		if err != nil {
			return "", fmt.Errorf("%w (`port install gh`)", err)
		}
		out, _, err := tool.Output(ctx, bin, tool.Opts{Args: args})
		if err != nil {
			return "", fmt.Errorf("gh %s: %s", args[0], err) //nolint:errorlint // not wrapped: the child's words survive as text and its identity does not; a child's exit status is not dockhand's to hand on
		}
		return string(out), nil
	}
}

// PullRequest is the slice of GitHub's PR object dockhand reads.
//
// The first five fields are what the tool has always read and what
// `status --json` publishes. MergeSha, CreatedAt and UpdatedAt are
// read for callers that have not arrived yet — the merge commit a
// landed branch can be confirmed against, and the two timestamps a
// staleness question would need. They cost nothing: the list-pulls
// response already carries all three, so no query asks for more than
// it did. A search result carries the timestamps but no merge commit,
// which is the same silence it already keeps about merged_at.
type PullRequest struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	State    string `json:"state"`
	MergedAt string `json:"merged_at"`
	HTMLURL  string `json:"html_url"`

	MergeSha  string `json:"merge_commit_sha"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// published is the document `status --json` has always emitted for a
// pull request: five keys, in this order, none omitted when empty.
type published struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	State    string `json:"state"`
	MergedAt string `json:"merged_at"`
	HTMLURL  string `json:"html_url"`
}

// MarshalJSON writes only the published five. Reading a field is a
// private matter between this package and GitHub, but a struct that is
// marshalled straight into `status --json` makes every field read a
// change to a document somebody's script parses. Widening that document
// is a decision for the verb that publishes it, not a side effect of
// this package learning to read one more key.
func (p PullRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(published{
		Number:   p.Number,
		Title:    p.Title,
		State:    p.State,
		MergedAt: p.MergedAt,
		HTMLURL:  p.HTMLURL,
	})
}

// LookupPR finds a promoted branch's pull request by head ref — the
// derived linkage: the tracking remote names the fork owner, and the
// query is the one gh itself uses. found is false for a branch never
// promoted or with no PR yet.
func LookupPR(ctx context.Context, gh Runner, repo *git.Repo, remotes map[string]string, upstream, branch string) (pr PullRequest, found bool, err error) {
	tracked := repo.TrackedRemote(ctx, branch)
	if tracked == "" {
		return PullRequest{}, false, nil
	}
	owner, _, ok := OwnerRepoFromURL(remotes[tracked])
	if !ok {
		return PullRequest{}, false, fmt.Errorf("cannot read an owner from remote %q", tracked)
	}
	return QueryPR(ctx, gh, upstream, owner, branch)
}

// QueryPR is the head-ref lookup itself, for callers that already know
// the fork owner — promote does, and a branch --replace just re-minted
// has no tracking config to derive it from until the push restores it.
func QueryPR(ctx context.Context, gh Runner, upstream, owner, branch string) (pr PullRequest, found bool, err error) {
	out, err := gh(ctx, "api",
		fmt.Sprintf("repos/%s/pulls?head=%s:%s&state=all", upstream, owner, branch))
	if err != nil {
		return PullRequest{}, false, err
	}
	var prs []PullRequest
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return PullRequest{}, false, fmt.Errorf("reading PR lookup: %w", err)
	}
	if len(prs) == 0 {
		return PullRequest{}, false, nil
	}
	return prs[0], true, nil
}

// ForkRemote finds the remote pointing at the user's own fork: the
// flag when given, otherwise the remote whose URL owner is the
// authenticated gh login. Requiring exactly one match keeps a
// many-remote checkout — other people's forks are remotes too — from
// being guessed at.
func ForkRemote(ctx context.Context, gh Runner, repo *git.Repo, override string) (name, owner string, err error) {
	remotes, err := repo.Remotes(ctx)
	if err != nil {
		return "", "", err
	}
	if override != "" {
		url, ok := remotes[override]
		if !ok {
			return "", "", fmt.Errorf("no remote %q", override)
		}
		owner, _, ok := OwnerRepoFromURL(url)
		if !ok {
			return "", "", fmt.Errorf("remote %q: cannot read an owner from %s", override, url)
		}
		return override, owner, nil
	}
	login, err := gh(ctx, "api", "user", "-q", ".login")
	if err != nil {
		return "", "", fmt.Errorf("finding your fork needs gh: %w (or pass --remote)", err)
	}
	login = strings.TrimSpace(login)
	var found []string
	for rname, url := range remotes {
		if o, _, ok := OwnerRepoFromURL(url); ok && o == login {
			found = append(found, rname)
		}
	}
	if len(found) != 1 {
		return "", "", fmt.Errorf("%d remotes belong to %s; pass --remote", len(found), login)
	}
	return found[0], login, nil
}
