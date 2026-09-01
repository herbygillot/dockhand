// Package forge is everything dockhand says to GitHub: the gh seam,
// pull-request lookup and search, fork and upstream resolution, and
// the PR body composed in the shape of macports-ports' own template.
// It sits above lifecycle (it reads verdict notes to compose bodies)
// and below cmd (which orchestrates the two).
package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/lifecycle"
)

// DuplicatePRError is promote's refusal when an open upstream PR
// already claims the same change: a duplicate spends reviewer
// attention on the purest kind of waste. Refusal with a remedy (exit
// 5), not a failure — the other PR may be theirs to join, or
// --no-pr-check promotes past it deliberately.
type DuplicatePRError struct {
	Title string
	URL   string
}

func (e *DuplicatePRError) Error() string {
	return fmt.Sprintf("an open PR already proposes %q: %s — join it, retitle with --title, or --no-pr-check to promote anyway", e.Title, e.URL)
}

// lintClause phrases a note's lint record for the evidence line.
func lintClause(lint string) string {
	if lint == "clean" {
		return "clean"
	}
	return "with " + lint
}

// OpenPortPRs lists the open upstream PRs whose titles claim the same
// port, leaning on the project convention that a title is
// `<port>: <description>` — dockhand's own titles included. The search
// API bounds and ranks the result; the prefix filter runs here because
// in:title matches the term anywhere in a title.
func OpenPortPRs(ctx context.Context, upstream, port string) ([]PullRequest, error) {
	if port == "" {
		return nil, nil
	}
	out, err := GhOut(ctx, "api", "-X", "GET", "search/issues",
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

// PromoteBody renders the PR body: what was done and what was — or
// was not — verified, stated plainly: candour is the accepted
// currency, and unverified assertions are what draw "did you verify
// this?".
// RepoURL is where the PR body's "dockhand" points, so a
// reviewer meeting the tool in a PR can see what vouched for the claim.
const RepoURL = "https://github.com/herbygillot/dockhand"

// PromoteBody renders the PR body in the shape of macports-ports' own
// pull request template, with the boxes dockhand can honestly vouch
// for checked and everything it cannot left for the human. Candour is
// the accepted currency: the verdict set is enumerated in full, an
// unverified promotion says so, and the install checkbox strikes the
// template's command through in favour of the one actually run.
func PromoteBody(n lifecycle.Note, verified bool, closes string, ownCommits int, checkedPRs bool) string {
	var b strings.Builder
	b.WriteString("#### Description\n\n")
	var passed []string
	tested, linted := false, false
	if !verified {
		b.WriteString("Not locally verified: no verification environment on the submitting machine.\n")
	} else {
		// The whole verdict set, enumerated: a passing platform and a
		// declining one are both facts a reviewer wants.
		var parts []string
		for _, plat := range n.Platforms() {
			r := n.Runs[plat]
			switch r.State {
			case "passed":
				what := "built in a pristine VM"
				if r.Tested {
					what, tested = "built and tested in a pristine VM", true
				}
				// The lint claim rides the evidence line, because the
				// checked box below is only honest if the body states
				// what backs it.
				switch {
				case r.Lint != "" && r.Linted:
					what, linted = "linted "+lintClause(r.Lint)+", "+what, true
				case r.Linted:
					what, linted = "linted, "+what, true
				}
				parts = append(parts, plat+": "+what)
				passed = append(passed, plat)
			case "unsupported":
				parts = append(parts, plat+": the port declines this platform (known_fail)")
			}
		}
		// One verdict per line: GitHub keeps single newlines in PR
		// bodies, so the set reads as the list it is.
		fmt.Fprintf(&b, "Verified with [dockhand](%s) at commit `%s`\n", RepoURL, n.Sha[:12])
		for _, part := range parts {
			fmt.Fprintf(&b, "  — %s.\n", part)
		}
	}
	if closes != "" {
		fmt.Fprintf(&b, "\nCloses: https://trac.macports.org/ticket/%s\n", closes)
	}

	b.WriteString("\n###### Type(s)\n\n- [ ] bugfix\n- [ ] enhancement\n- [ ] security fix\n")
	if len(passed) > 0 {
		b.WriteString("\n###### Tested on\n")
		for _, plat := range passed {
			fmt.Fprintf(&b, "- macOS %s — pristine tart VM, via dockhand\n", plat)
		}
	}

	box := func(ok bool, item string) {
		mark := " "
		if ok {
			mark = "x"
		}
		fmt.Fprintf(&b, "- [%s] %s\n", mark, item)
	}
	// The single lifecycle.Minted commit is the one whose message dockhand wrote
	// in project format; a branch the user grew past it is theirs to
	// vouch for.
	single := ownCommits == 1
	b.WriteString("\n###### Verification\nHave you\n\n")
	box(single, "followed our [Commit Message Guidelines](https://trac.macports.org/wiki/CommitMessages)?")
	box(single, "squashed and [minimized your commits](https://guide.macports.org/#project.github)?")
	box(checkedPRs, "checked that there aren't other open [pull requests](https://github.com/macports/macports-ports/pulls) for the same change?")
	box(false, "referenced existing tickets on [Trac](https://trac.macports.org/wiki/Tickets) with full URL in commit message?")
	box(linted, "checked your Portfile with `port lint`?")
	box(tested, "tried existing tests with `sudo port test`?")
	box(len(passed) > 0, "tried a full install with ~~`sudo port -vst install`~~ `sudo port install` in a pristine VM")
	box(false, "tested basic functionality of all binary files?")
	box(false, "checked that the Portfile's most important [variants](https://trac.macports.org/wiki/Variants) haven't been broken?")
	// Every body signs off, the unverified ones included: a PR with no
	// verification claim still owes the reviewer the fact of how it was
	// made.
	fmt.Fprintf(&b, "\nAutomated by [dockhand](%s)\n", RepoURL)
	return b.String()
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

// GhOut runs one gh command and returns its stdout. A variable so the
// promote lifecycle tests can stand in a scripted GitHub — the same
// seam shape lifecycle.VMProvider gave the verifier.
var GhOut func(ctx context.Context, args ...string) (string, error) = realGhOut

func realGhOut(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("gh %s: %s", args[0], msg)
	}
	return out.String(), nil
}

// PullRequest is the slice of GitHub's PR object clean reads.
type PullRequest struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	State    string `json:"state"`
	MergedAt string `json:"merged_at"`
	HTMLURL  string `json:"html_url"`
}

// LookupPR finds a promoted branch's pull request by head ref — the
// derived linkage: the tracking remote names the fork owner, and the
// query is the one gh itself uses. found is false for a branch never
// promoted or with no PR yet.
func LookupPR(ctx context.Context, repo *git.Repo, remotes map[string]string, upstream, branch string) (pr PullRequest, found bool, err error) {
	tracked := repo.TrackedRemote(ctx, branch)
	if tracked == "" {
		return PullRequest{}, false, nil
	}
	owner, _, ok := OwnerRepoFromURL(remotes[tracked])
	if !ok {
		return PullRequest{}, false, fmt.Errorf("cannot read an owner from remote %q", tracked)
	}
	return QueryPR(ctx, upstream, owner, branch)
}

// QueryPR is the head-ref lookup itself, for callers that already know
// the fork owner — promote does, and a branch --force just re-lifecycle.Minted
// has no tracking config to derive it from until the push restores it.
func QueryPR(ctx context.Context, upstream, owner, branch string) (pr PullRequest, found bool, err error) {
	out, err := GhOut(ctx, "api",
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
func ForkRemote(ctx context.Context, repo *git.Repo, override string) (name, owner string, err error) {
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
	login, err := GhOut(ctx, "api", "user", "-q", ".login")
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

// Promote builds the promote subcommand.
