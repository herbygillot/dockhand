package cmd

import (
	"bytes"
	"context"
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
	target   string
	remote   string // fork remote; empty means detect by gh login
	title    string
	closes   string
	noPR     bool
	noVerify bool // promote an unverified tip deliberately
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
	if err := repo.Push(ctx, forkRemote, branch); err != nil {
		return err
	}
	fmt.Fprintf(rs.Err, "pushed %s to %s (%s)\n", branch, forkRemote, forkOwner)
	if a.noPR {
		return nil
	}

	upstream, err := upstreamRepo(ctx, repo)
	if err != nil {
		return err
	}
	title := a.title
	if title == "" {
		// The tip commit's subject is already in project format, and
		// the PR template auto-detects the change type from the title.
		title, err = repoSubject(ctx, repo, tip)
		if err != nil {
			return err
		}
	}
	body := promoteBody(n, verified, a.closes)
	args := []string{"pr", "create", "--repo", upstream,
		"--head", forkOwner + ":" + branch, "--title", title, "--body", body}
	url, err := ghOut(ctx, args...)
	if err != nil {
		return fmt.Errorf("the branch is pushed; opening the PR failed: %w", err)
	}
	fmt.Fprintln(rs.Out, strings.TrimSpace(url))
	return nil
}

// promoteBody renders the PR body: what was done and what was — or
// was not — verified, stated plainly: candour is the accepted
// currency, and unverified assertions are what draw "did you verify
// this?".
func promoteBody(n verifyNote, verified bool, closes string) string {
	var b strings.Builder
	if !verified {
		b.WriteString("Not locally verified: no verification environment on the submitting machine.\n")
	} else {
		// The whole verdict set, enumerated: a passing platform and a
		// declining one are both facts a reviewer wants, and candour is
		// the accepted currency.
		var parts []string
		for _, plat := range n.platforms() {
			switch n.Runs[plat].State {
			case "passed":
				parts = append(parts, plat+": built in a pristine VM")
			case "unsupported":
				parts = append(parts, plat+": the port declines this platform (known_fail)")
			}
		}
		fmt.Fprintf(&b, "Verified with dockhand at commit `%s` — %s.\n",
			n.Sha[:12], strings.Join(parts, "; "))
	}
	if closes != "" {
		fmt.Fprintf(&b, "\nCloses: https://trac.macports.org/ticket/%s\n", closes)
	}
	return b.String()
}

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
		return "", fmt.Errorf("%q names %d branches (%s); use the full branch name", target, len(matches), strings.Join(matches, ", "))
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

// repoSubject is the tip commit's subject line.
func repoSubject(ctx context.Context, repo *git.Repo, sha string) (string, error) {
	out, err := ghlessGit(ctx, repo, "log", "-1", "--format=%s", sha)
	return out, err
}

// ghlessGit is a tiny indirection so promote's one log read shares the
// repo's plumbing without widening the git package for a subject line.
func ghlessGit(ctx context.Context, repo *git.Repo, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo.Root}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

// ghOut runs one gh command and returns its stdout.
func ghOut(ctx context.Context, args ...string) (string, error) {
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
		remote   string
		title    string
		closes   string
		noPR     bool
		noVerify bool
	)
	c := &cobra.Command{
		Use:   "promote <branch|port>",
		Short: "Push a verified branch to your fork and open the pull request",
		Args:  exactArgs(1),
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return promoteAction{
				target: args[0], remote: remote,
				title: title, closes: closes, noPR: noPR, noVerify: noVerify,
			}, nil
		}),
	}
	c.Flags().StringVar(&remote, "remote", "", "the fork remote to push to (default: the remote owned by your gh login)")
	c.Flags().StringVar(&title, "title", "", "PR title (default: the tip commit's subject)")
	c.Flags().StringVar(&closes, "closes", "", "Trac ticket number the PR closes")
	c.Flags().BoolVar(&noPR, "no-pr", false, "push to the fork without opening a pull request")
	c.Flags().BoolVar(&noVerify, "no-verify", false,
		"promote even if the branch is unverified; the PR discloses it")
	return c
}
