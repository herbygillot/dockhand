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

// MaxPRBody is the most characters GitHub accepts in a pull request
// body. The number is GitHub's and not gh's: the API refuses a longer
// body with "Body is too long (maximum is 65536 characters)", and gh
// relays that sentence verbatim — after `gh pr create` has been handed
// a branch that is already on the fork, which is why promote measures
// against this before it pushes rather than learning it afterwards.
//
// The unit is the one GitHub's own message uses, characters, so a body
// is measured in code points and not bytes. The difference is real on
// this template: every evidence line carries an em-dash, and a count
// in bytes would refuse a body GitHub takes.
//
// It is stated here because it is a fact about the forge, the same as
// the page size below; this package keeps no opinion about what to do
// with it.
const MaxPRBody = 65536

// openPRPageSize is how many pull requests one page asks for, which is
// the REST list endpoint's own maximum. Fewer pages is fewer round
// trips, and the response is already the shape this package reads.
const openPRPageSize = 100

// openPRPageLimit bounds the walk. It is a guard against a paging bug
// looping forever against a live API, not a budget: at a hundred per
// page it covers ten thousand open pull requests, which is an order of
// magnitude past any ports tree. Reaching it is an error and never a
// short answer, for the reason stated on OpenPortPRs.
const openPRPageLimit = 100

// OpenPortPRs lists the open upstream PRs whose titles claim the same
// port, leaning on the project convention that a title is
// `<port>: <description>` — dockhand's own titles included.
//
// It walks `pulls?state=open` rather than asking the search API, which
// is what it used to do. Search has a rate limit of its own, an order of
// magnitude tighter than the REST one, and it is spent per promote —
// which was affordable while every promote was a person typing a verb
// and stopped being affordable the moment an unattended pass began
// running the same check once per branch. On the unattended road a
// rate-limited lookup is not an advisory: it refuses the publication.
// Trading a bounded, ranked search for a complete walk buys the check
// the quota it needs and, incidentally, makes it exact — in:title
// matched the term anywhere in a title, which is why the prefix filter
// below existed before the walk did.
//
// PAGING IS COMPLETE OR IT IS AN ERROR. A truncated walk reads as "no
// duplicate found", which is the one wrong answer this check can give:
// it opens a second pull request beside somebody's first. So a page the
// forge would not serve is returned as a failure, and so is a walk that
// runs past its bound.
func OpenPortPRs(ctx context.Context, gh Runner, upstream, port string) ([]PullRequest, error) {
	if port == "" {
		return nil, nil
	}
	var prs []PullRequest
	for page := 1; page <= openPRPageLimit; page++ {
		out, err := gh(ctx, "api", fmt.Sprintf("repos/%s/pulls?state=open&per_page=%d&page=%d",
			upstream, openPRPageSize, page))
		if err != nil {
			return nil, err
		}
		var batch []PullRequest
		if err := json.Unmarshal([]byte(out), &batch); err != nil {
			return nil, fmt.Errorf("reading open PRs: %w", err)
		}
		for _, pr := range batch {
			if strings.HasPrefix(pr.Title, port+":") {
				prs = append(prs, pr)
			}
		}
		// A short page is the last page. The endpoint carries a Link
		// header saying so too, and `gh api` without --paginate does not
		// hand it back — the count is the part of the answer this seam can
		// see, and it is exact at both ends: a full last page costs one
		// more request that comes back empty.
		if len(batch) < openPRPageSize {
			return prs, nil
		}
	}
	return nil, fmt.Errorf("listing open PRs on %s: more than %d pages", upstream, openPRPageLimit)
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

	// Head is the branch the pull request proposes from. It is read for
	// one thing: a branch dockhand minted carries, in its name, the
	// version the change takes its port to, and promote's same-port
	// advisory reads that back so it can say whether somebody's open
	// pull request already goes as far as this one. What the name means
	// is decided at the engine boundary, where the mint's construction
	// is known — this package hands the ref over and holds no opinion
	// about it, on the same terms as every other field here.
	Head PRHead `json:"head"`
}

// PRHead is the part of a pull request's head that dockhand reads —
// the branch name. GitHub sends the sha and the repository beside it,
// and neither is read: a ref is enough to invert a branch name dockhand
// minted, and the tree behind it is the dearer source docs/todo.md
// leaves for later.
type PRHead struct {
	Ref string `json:"ref"`
}

// published is the document `status --json` has always emitted for a
// pull request: five keys, in this order, none omitted when empty.
//
// created_at is deliberately NOT among them, and it is worth saying so
// now that the human report's ORDER depends on it: the attention bands
// include "open past its 72-hour review window", which is a function of
// that timestamp and the clock, so a consumer of the document sees a
// reordered array it cannot reproduce. The array order is explicitly not
// part of the contract (render.Report), so this is a gap rather than a
// break — but widening a published document is the verb's decision and
// not this package's, and it is not one an ordering change may make on
// its way past.
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

// LookupPR finds a pushed branch's pull request by head ref — the
// derived linkage: remote is the one holding the branch's copy (what
// git.PushedTo names), its URL's owner is the fork owner, and the query
// is the one gh itself uses. found is false for a branch with no PR
// yet.
//
// The remote is the caller's to name rather than read from the
// branch's tracking config here, because the config is not where the
// copy is: a branch pushed bare tracks nothing, and one cut from a
// remote-tracking base tracks a remote it was never sent to.
func LookupPR(ctx context.Context, gh Runner, remotes map[string]string, remote, upstream, branch string) (pr PullRequest, found bool, err error) {
	owner, _, ok := OwnerRepoFromURL(remotes[remote])
	if !ok {
		return PullRequest{}, false, fmt.Errorf("cannot read an owner from remote %q", remote)
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
