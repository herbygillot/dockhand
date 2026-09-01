package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// promoteAction publishes a verified branch: push it to the user's
// fork under its own name, then open the pull request against the
// upstream repository. The PR is ring 3 — other people's attention —
// and this is the only verb that spends it (cli.md); everything before
// the PR is the user's own fork, deletable at will.
//
// Nothing is stored: the branch→PR link is derived (D21) — the push
// writes ordinary tracking config, and any later lookup queries pulls
// by head ref, the same way gh itself does.
type promoteAction struct {
	target    string
	remote    string // fork remote; empty means detect by gh login
	title     string
	closes    string
	noPR      bool
	noVerify  bool // promote an unverified tip deliberately
	noPRCheck bool // skip the duplicate-PR search deliberately
	force     bool // replace the fork branch and refresh the PR
}

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

var _ Action = promoteAction{}

func (a promoteAction) Execute(ctx context.Context, rs *runstate.Context) error {
	repo, err := rs.Repo(ctx)
	if err != nil {
		return err
	}
	branch, err := resolveDockhandBranch(ctx, repo, a.target)
	if err != nil {
		return err
	}
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return err
	}

	// Promote refuses an unverified tip: the PR spends reviewer
	// attention, and the private backends exist to predict the shared
	// one's verdict before that happens. Without a local verify
	// provider there is nothing to refuse toward — the machine cannot
	// verify — so the promotion proceeds unverified, says so, and the
	// PR body says so too, which is the candour reviewers accept.
	n, verified, err := promotableVerdictFor(ctx, repo, tip)
	if err != nil {
		return err
	}
	if !verified {
		reason := "is unverified"
		if n.anyState("failed") {
			reason = "has a failed verification"
		}
		switch {
		case a.noVerify:
			fmt.Fprintln(rs.Err, "promoting unverified (--no-verify); the PR will say so")
		case tartPresent():
			return fmt.Errorf("%s: tip %s %s — `dockhand verify %s` first, or --no-verify to promote anyway", branch, tip[:12], reason, branch)
		default:
			fmt.Fprintln(rs.Err, "no local verify provider (tart): promoting unverified")
		}
	}

	forkRemote, forkOwner, err := a.forkRemote(ctx, repo)
	if err != nil {
		return err
	}
	if a.noPR {
		if err := a.push(ctx, rs, repo, forkRemote, forkOwner, branch); err != nil {
			return err
		}
		return nil
	}

	upstream, err := upstreamRepo(ctx, repo)
	if err != nil {
		return err
	}
	// The branch's own commits, oldest last (rev-list order): the
	// oldest is the one dockhand minted, and its subject is already in
	// project format (`<port>: <description>`) — later commits are
	// fixups whose subjects would make bad titles. The count also
	// answers the template's squashed-and-minimized checkbox.
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return err
	}
	own, err := repo.OwnCommits(ctx, tip, primary)
	if err != nil {
		return err
	}
	title := a.title
	if title == "" {
		subject := tip
		if len(own) > 0 {
			subject = own[len(own)-1]
		}
		title, err = repo.Subject(ctx, subject)
		if err != nil {
			return err
		}
	}
	// A branch that already has its own open PR is re-promotion, not
	// duplication: the push below updates that PR in place, and opening
	// a second one would be the duplicate this verb refuses elsewhere.
	// Looked up by the fork owner, never by tracking config — a branch
	// --force just re-minted has none until the push restores it.
	ownPR, ownFound, err := queryPR(ctx, upstream, forkOwner, branch)
	if err != nil {
		fmt.Fprintf(rs.Err, "warning: could not check for this branch's own PR: %v\n", err)
		ownFound = false
	}
	if ownFound && ownPR.MergedAt != "" {
		return fmt.Errorf("PR #%d for %s already merged (%s) — `dockhand clean` retires the branch", ownPR.Number, branch, ownPR.HTMLURL)
	}

	checkedPRs := false
	if !a.noPRCheck {
		port := n.Port
		if before, _, found := strings.Cut(title, ":"); port == "" && found {
			port = strings.TrimSpace(before)
		}
		switch prs, serr := openPortPRs(ctx, upstream, port); {
		case port == "":
			fmt.Fprintln(rs.Err, "warning: no port name to search open PRs by; skipping the duplicate check")
		case serr != nil:
			// The search is advisory: a rate-limited or offline lookup
			// must not block a promotion, it just leaves the checklist
			// box for the human.
			fmt.Fprintf(rs.Err, "warning: could not search for open PRs: %v\n", serr)
		default:
			checkedPRs = true
			for _, pr := range prs {
				if ownFound && pr.Number == ownPR.Number {
					continue
				}
				if strings.EqualFold(strings.TrimSpace(pr.Title), strings.TrimSpace(title)) {
					return &DuplicatePRError{Title: pr.Title, URL: pr.HTMLURL}
				}
				// Same port, different change: not a duplicate, but a
				// maintainer coordinating both will want to know now
				// rather than at review.
				fmt.Fprintf(rs.Err, "note: an open PR already touches this port: #%d %q (%s)\n", pr.Number, pr.Title, pr.HTMLURL)
			}
		}
	}

	if err := a.push(ctx, rs, repo, forkRemote, forkOwner, branch); err != nil {
		return err
	}
	body := promoteBody(n, verified, a.closes, len(own), checkedPRs)
	if ownFound && ownPR.State == "open" {
		if a.force {
			// A replaced branch usually means a new version: the PR's
			// commits moved with the push, and its title and body are
			// stale until told otherwise.
			if _, err := ghOut(ctx, "pr", "edit", fmt.Sprint(ownPR.Number), "--repo", upstream,
				"--title", title, "--body", body); err != nil {
				return fmt.Errorf("the branch is pushed; refreshing PR #%d failed: %w", ownPR.Number, err)
			}
			fmt.Fprintf(rs.Err, "PR #%d replaced: branch force-pushed, title and body refreshed\n", ownPR.Number)
		} else {
			fmt.Fprintf(rs.Err, "PR #%d already open for this branch; the push updated it\n", ownPR.Number)
		}
		fmt.Fprintln(rs.Out, ownPR.HTMLURL)
		return nil
	}

	args := []string{"pr", "create", "--repo", upstream,
		"--head", forkOwner + ":" + branch, "--title", title, "--body", body}
	url, err := ghOut(ctx, args...)
	if err != nil {
		return fmt.Errorf("the branch is pushed; opening the PR failed: %w", err)
	}
	fmt.Fprintln(rs.Out, strings.TrimSpace(url))
	return nil
}

// lintClause phrases a note's lint record for the evidence line.
func lintClause(lint string) string {
	if lint == "clean" {
		return "clean"
	}
	return "with " + lint
}

// push publishes the branch to the fork: an ordinary push, or the
// with-lease force that replaces a re-minted branch's copy.
func (a promoteAction) push(ctx context.Context, rs *runstate.Context, repo *git.Repo, remote, owner, branch string) error {
	if a.force {
		if err := repo.PushForce(ctx, remote, branch); err != nil {
			return err
		}
		fmt.Fprintf(rs.Err, "force-pushed %s to %s (%s)\n", branch, remote, owner)
		return nil
	}
	if err := repo.Push(ctx, remote, branch); err != nil {
		return err
	}
	fmt.Fprintf(rs.Err, "pushed %s to %s (%s)\n", branch, remote, owner)
	return nil
}

// openPortPRs lists the open upstream PRs whose titles claim the same
// port, leaning on the project convention that a title is
// `<port>: <description>` — dockhand's own titles included. The search
// API bounds and ranks the result; the prefix filter runs here because
// in:title matches the term anywhere in a title.
func openPortPRs(ctx context.Context, upstream, port string) ([]pullRequest, error) {
	if port == "" {
		return nil, nil
	}
	out, err := ghOut(ctx, "api", "-X", "GET", "search/issues",
		"-f", fmt.Sprintf("q=repo:%s is:pr is:open in:title %q", upstream, port+":"))
	if err != nil {
		return nil, err
	}
	var res struct {
		Items []pullRequest `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("reading PR search: %w", err)
	}
	var prs []pullRequest
	for _, pr := range res.Items {
		if strings.HasPrefix(pr.Title, port+":") {
			prs = append(prs, pr)
		}
	}
	return prs, nil
}

// promoteBody renders the PR body: what was done and what was — or
// was not — verified, stated plainly: candour is the accepted
// currency, and unverified assertions are what draw "did you verify
// this?".
// dockhandRepoURL is where the PR body's "dockhand" points, so a
// reviewer meeting the tool in a PR can see what vouched for the claim.
const dockhandRepoURL = "https://github.com/herbygillot/dockhand"

// promoteBody renders the PR body in the shape of macports-ports' own
// pull request template, with the boxes dockhand can honestly vouch
// for checked and everything it cannot left for the human. Candour is
// the accepted currency: the verdict set is enumerated in full, an
// unverified promotion says so, and the install checkbox strikes the
// template's command through in favour of the one actually run.
func promoteBody(n verifyNote, verified bool, closes string, ownCommits int, checkedPRs bool) string {
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
		for _, plat := range n.platforms() {
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
		fmt.Fprintf(&b, "Verified with [dockhand](%s) at commit `%s`\n", dockhandRepoURL, n.Sha[:12])
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
	// The single minted commit is the one whose message dockhand wrote
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
	fmt.Fprintf(&b, "\nAutomated by [dockhand](%s)\n", dockhandRepoURL)
	return b.String()
}

// errAmbiguousTarget marks a port name that names several in-flight
// branches: branchable state, because verify falls through to state
// mode when no branch exists but must refuse — not silently verify the
// working tree — when several do.
var errAmbiguousTarget = errors.New("ambiguous target")

// resolveDockhandBranch accepts a branch name outright, or a port name
// that names exactly one in-flight branch.
func resolveDockhandBranch(ctx context.Context, repo *git.Repo, target string) (string, error) {
	if repo.HasBranch(ctx, target) {
		return target, nil
	}
	branches, err := repo.Branches(ctx, "dockhand/")
	if err != nil {
		return "", err
	}
	var matches []string
	for _, br := range branches {
		if strings.HasPrefix(br, "dockhand/"+target+"-") {
			matches = append(matches, br)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no dockhand branch for %q; `dockhand status` lists what is in flight", target)
	default:
		return "", fmt.Errorf("%w: %q names %d branches (%s); use the full branch name", errAmbiguousTarget, target, len(matches), strings.Join(matches, ", "))
	}
}

// forkRemote finds the remote pointing at the user's own fork: the
// flag when given, otherwise the remote whose URL owner is the
// authenticated gh login. Requiring exactly one match keeps a
// many-remote checkout — other people's forks are remotes too — from
// being guessed at.
func (a promoteAction) forkRemote(ctx context.Context, repo *git.Repo) (name, owner string, err error) {
	remotes, err := repo.Remotes(ctx)
	if err != nil {
		return "", "", err
	}
	if a.remote != "" {
		url, ok := remotes[a.remote]
		if !ok {
			return "", "", fmt.Errorf("no remote %q", a.remote)
		}
		owner, _, ok := ownerRepoFromURL(url)
		if !ok {
			return "", "", fmt.Errorf("remote %q: cannot read an owner from %s", a.remote, url)
		}
		return a.remote, owner, nil
	}
	login, err := ghOut(ctx, "api", "user", "-q", ".login")
	if err != nil {
		return "", "", fmt.Errorf("finding your fork needs gh: %w (or pass --remote)", err)
	}
	login = strings.TrimSpace(login)
	var found []string
	for rname, url := range remotes {
		if o, _, ok := ownerRepoFromURL(url); ok && o == login {
			found = append(found, rname)
		}
	}
	if len(found) != 1 {
		return "", "", fmt.Errorf("%d remotes belong to %s; pass --remote", len(found), login)
	}
	return found[0], login, nil
}

// upstreamRepo names the owner/repo the PR targets: the remote the
// primary branch tracks — where the work forked from is where it goes
// back to.
func upstreamRepo(ctx context.Context, repo *git.Repo) (string, error) {
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
	owner, name, ok := ownerRepoFromURL(remotes[remote])
	if !ok {
		return "", fmt.Errorf("cannot read owner/repo from remote %q (%s)", remote, remotes[remote])
	}
	return owner + "/" + name, nil
}

// ownerRepoFromURL reads owner and repository out of a git remote URL,
// in both the ssh (git@host:owner/repo.git) and https
// (https://host/owner/repo) spellings.
func ownerRepoFromURL(url string) (owner, repo string, ok bool) {
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

// ghOut runs one gh command and returns its stdout. A variable so the
// promote lifecycle tests can stand in a scripted GitHub — the same
// seam shape vmProvider gave the verifier.
var ghOut func(ctx context.Context, args ...string) (string, error) = realGhOut

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

// Promote builds the promote subcommand.
func Promote() *cobra.Command {
	var (
		remote    string
		title     string
		closes    string
		noPR      bool
		noVerify  bool
		noPRCheck bool
		force     bool
	)
	c := &cobra.Command{
		Use:   "promote <branch|port>",
		Short: "Push a verified branch to your fork and open the pull request",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return promoteAction{
				target: args[0], remote: remote,
				title: title, closes: closes, noPR: noPR, noVerify: noVerify,
				noPRCheck: noPRCheck, force: force,
			}, nil
		}),
	}
	c.Flags().StringVar(&remote, "remote", "", "the fork remote to push to (default: the remote owned by your gh login)")
	c.Flags().StringVar(&title, "title", "", "PR title (default: the tip commit's subject)")
	c.Flags().StringVar(&closes, "closes", "", "Trac ticket number the PR closes")
	c.Flags().BoolVar(&noPR, "no-pr", false, "push to the fork without opening a pull request")
	c.Flags().BoolVar(&noVerify, "no-verify", false,
		"promote even if the branch is unverified; the PR discloses it")
	c.Flags().BoolVar(&noPRCheck, "no-pr-check", false,
		"skip the search for pre-existing open PRs on the same port")
	c.Flags().BoolVar(&force, "force", false,
		"replace the fork branch (force-push with lease) and refresh the open PR's title and body")
	return c
}
