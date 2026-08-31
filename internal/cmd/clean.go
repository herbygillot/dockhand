package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// cleanAction sweeps the dockhand/* namespace by evidence: a branch
// whose pull request merged is done, and everything it holds is
// released through the same demolition discard uses. Merged-ness is
// never sha ancestry — the project's merge styles rewrite commits as
// they land, so `git branch --merged` sees nothing; the PR's own state
// decides, confirmed by comparing the touched files' bytes against the
// primary branch. Every deletion is reported; everything kept says
// why. The branch→PR link is derived, never stored: the tracking
// remote names the fork owner, and the PR is found by head ref — the
// lookup gh itself uses. Head-ref queries return a bounded handful,
// which is what keeps this sweep safe from the silent-truncation trap
// a bulk PR listing invites.
type cleanAction struct{}

var _ Action = cleanAction{}

func (cleanAction) Execute(ctx context.Context, rs *runstate.Context) error {
	dir := rs.TreeRoot
	if dir == "" {
		dir = "."
	}
	repo, err := git.Open(ctx, dir)
	if err != nil {
		return err
	}
	branches, err := repo.Branches(ctx, "dockhand/")
	if err != nil {
		return err
	}
	if len(branches) == 0 {
		fmt.Fprintln(rs.Out, "no dockhand branches")
		return nil
	}
	upstream, err := upstreamRepo(ctx, repo)
	if err != nil {
		return err
	}
	remotes, err := repo.Remotes(ctx)
	if err != nil {
		return err
	}
	for _, br := range branches {
		line, err := cleanOne(ctx, rs, repo, remotes, upstream, br)
		if err != nil {
			line = "error: " + err.Error()
		}
		fmt.Fprintf(rs.Out, "%-32s %s\n", br, line)
	}
	return nil
}

// pullRequest is the slice of GitHub's PR object clean reads.
type pullRequest struct {
	Number   int    `json:"number"`
	State    string `json:"state"`
	MergedAt string `json:"merged_at"`
	HTMLURL  string `json:"html_url"`
}

// cleanOne judges one branch and acts only on the merged verdict.
func cleanOne(ctx context.Context, rs *runstate.Context, repo *git.Repo, remotes map[string]string, upstream, branch string) (string, error) {
	tracked := repo.TrackedRemote(ctx, branch)
	if tracked == "" {
		return "kept — never promoted", nil
	}
	owner, _, ok := ownerRepoFromURL(remotes[tracked])
	if !ok {
		return "", fmt.Errorf("cannot read an owner from remote %q", tracked)
	}
	out, err := ghOut(ctx, "api",
		fmt.Sprintf("repos/%s/pulls?head=%s:%s&state=all", upstream, owner, branch))
	if err != nil {
		return "", err
	}
	var prs []pullRequest
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return "", fmt.Errorf("reading PR lookup: %w", err)
	}
	if len(prs) == 0 {
		return "kept — promoted, but no PR found", nil
	}
	pr := prs[0]
	switch {
	case pr.MergedAt != "":
		identical, err := contentLanded(ctx, repo, branch)
		if err != nil {
			return "", err
		}
		if err := discardBranch(ctx, rs, repo, branch); err != nil {
			return "", err
		}
		verdict := fmt.Sprintf("cleaned — PR #%d merged", pr.Number)
		if !identical {
			// Merged is the authority; differing bytes mean a committer
			// amended in flight or a later change superseded it — worth
			// saying, never worth keeping the branch for.
			verdict += " (upstream bytes differ: amended on merge, or since superseded)"
		}
		return verdict, nil
	case pr.State == "open":
		return fmt.Sprintf("kept — PR #%d open (%s)", pr.Number, pr.HTMLURL), nil
	default:
		return fmt.Sprintf("kept — PR #%d closed without merging; rejection is information", pr.Number), nil
	}
}

// contentLanded reports whether every file the branch touched reads
// byte-identical on the primary branch — the confirmation half of the
// merged verdict, at the branch's local view of upstream.
func contentLanded(ctx context.Context, repo *git.Repo, branch string) (bool, error) {
	primary, err := repo.PrimaryBranch(ctx)
	if err != nil {
		return false, err
	}
	base, err := repo.MergeBase(ctx, primary, branch)
	if err != nil {
		return false, err
	}
	paths, err := repo.DiffNames(ctx, base, branch)
	if err != nil {
		return false, err
	}
	for _, p := range paths {
		if strings.HasPrefix(p, "_") {
			continue
		}
		ours, err := repo.BlobAt(ctx, branch, p)
		if err != nil {
			return false, err
		}
		theirs, err := repo.BlobAt(ctx, primary, p)
		if err != nil || string(ours) != string(theirs) {
			return false, nil
		}
	}
	return true, nil
}

// Clean builds the clean subcommand. The verb borrows `port clean`
// safely: both mean removing the tool's own accumulated work-product.
func Clean() *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: "Sweep away branches whose pull requests merged",
		Args:  noArgs,
		RunE: runE(func(*cobra.Command, []string) (Action, error) {
			return cleanAction{}, nil
		}),
	}
}
