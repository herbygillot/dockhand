package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/tart"
)

// statusAction reconciles the dockhand/* namespace: every branch, its
// tip's verification record, and the drift between them. It is a
// reconciler, not a daemon: running jobs are polled here, their
// verdicts written back to the notes, workers released on pass, and —
// the one deletion status performs — a branch whose PR merged is
// cleaned, announced, because a merged PR is GitHub's own word that
// the work landed. Every other cleanup is the user's explicit act.
type statusAction struct{}

var _ Action = statusAction{}

func (statusAction) Execute(ctx context.Context, rs *runstate.Context) error {
	repo, err := rs.Repo(ctx)
	if err != nil {
		return err
	}
	branches, err := repo.Branches(ctx, "dockhand/")
	if err != nil {
		return err
	}
	if len(branches) == 0 {
		// Naming the repository is the point: run from the wrong
		// checkout, "no branches" is true and useless — true and
		// located is actionable.
		fmt.Fprintf(rs.Out, "no dockhand branches in %s\n", repo.Root)
		reportOrphanWorkers(ctx, rs, repo)
		return nil
	}
	pr := newPRStatus(ctx, repo)
	for _, br := range branches {
		lines, err := describeBranch(ctx, repo, br)
		if err != nil {
			lines = []string{"error: " + err.Error()}
		}
		if cleaned, extra := pr.reconcile(ctx, rs, br); cleaned {
			lines = []string{extra}
		} else if extra != "" {
			lines = append(lines, extra)
		}
		if len(lines) == 1 {
			fmt.Fprintf(rs.Out, "%-32s %s\n", br, lines[0])
			continue
		}
		fmt.Fprintln(rs.Out, br)
		for _, l := range lines {
			fmt.Fprintf(rs.Out, "  %s\n", l)
		}
	}
	reportOrphanWorkers(ctx, rs, repo)
	return nil
}

// prStatus carries the lazily-fetched pieces the PR half of status
// needs: the upstream repo and the remote table, resolved once.
type prStatus struct {
	repo     *git.Repo
	upstream string
	remotes  map[string]string
	broken   error
	loaded   bool
}

func newPRStatus(ctx context.Context, repo *git.Repo) *prStatus {
	return &prStatus{repo: repo}
}

// reconcile reports a promoted branch's PR standing — and when the PR
// merged, the branch is done: status says so and cleans it, the one
// deletion status performs, because a merged PR is GitHub's own word
// that the work landed. Everything else stays reporting: open PRs,
// closed ones, and unpromoted branches say their state and stand.
func (ps *prStatus) reconcile(ctx context.Context, rs *runstate.Context, branch string) (cleaned bool, line string) {
	if ps.repo.TrackedRemote(ctx, branch) == "" {
		return false, ""
	}
	if !ps.loaded {
		ps.loaded = true
		ps.upstream, ps.broken = upstreamRepo(ctx, ps.repo)
		if ps.broken == nil {
			ps.remotes, ps.broken = ps.repo.Remotes(ctx)
		}
	}
	if ps.broken != nil {
		return false, "PR state unavailable: " + ps.broken.Error()
	}
	pr, found, err := lookupPR(ctx, ps.repo, ps.remotes, ps.upstream, branch)
	if err != nil {
		return false, "PR state unavailable: " + err.Error()
	}
	if !found {
		return false, "promoted; no PR found"
	}
	switch {
	case pr.MergedAt != "":
		if err := discardBranch(ctx, rs, ps.repo, branch); err != nil {
			return false, fmt.Sprintf("PR #%d merged; cleaning failed: %v", pr.Number, err)
		}
		return true, fmt.Sprintf("PR #%d merged — branch cleaned", pr.Number)
	case pr.State == "open":
		return false, fmt.Sprintf("PR #%d open (%s)", pr.Number, pr.HTMLURL)
	default:
		return false, fmt.Sprintf("PR #%d closed without merging", pr.Number)
	}
}

// describeBranch renders one branch's verification standing, polling
// and settling whatever is still running on its tip.
func describeBranch(ctx context.Context, repo *git.Repo, branch string) ([]string, error) {
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return nil, err
	}
	n, err := readNote(ctx, repo, tip)
	if errors.Is(err, git.ErrNoNote) {
		line, err := describeUnverifiedTip(ctx, repo, branch, tip)
		if err != nil {
			return nil, err
		}
		return []string{line}, nil
	}
	if err != nil {
		return nil, err
	}
	if n.anyState("running") {
		if err := settleRuns(ctx, repo, &n); err != nil {
			return nil, err
		}
	}
	return renderNote(n), nil
}

// settleRuns polls every running run and writes what it learns back to
// the note. Poll never mutates and Release is the caller's: status
// releases the worker on pass — a kept green environment is a wasted
// slot — and keeps it on failure, where it is the debug handle. A
// failure whose log shows the port refusing the platform records as
// unsupported instead, and its worker is released: a correct refusal
// leaves nothing to debug.
func settleRuns(ctx context.Context, repo *git.Repo, n *verifyNote) error {
	prov, err := vmProvider(ctx)
	if err != nil {
		return nil // running, cannot poll; the note stands as is
	}
	changed := false
	for plat, r := range n.Runs {
		if r.State != "running" {
			continue
		}
		st, perr := prov.Poll(ctx, r.Job)
		if errors.Is(perr, verify.ErrUnknownJob) {
			r.State, r.Detail = "errored", "job vanished: its worker no longer exists"
			n.Runs[plat], changed = r, true
			continue
		}
		if perr != nil {
			return perr
		}
		switch st.State {
		case verify.Running:
			continue
		case verify.Passed:
			r.State = "passed"
			if rerr := prov.Release(ctx, r.Job); rerr != nil {
				r.Detail = "worker not released: " + rerr.Error()
			}
		case verify.Failed:
			r.State, r.Handle = "failed", st.Handle
			if log, lerr := prov.Log(ctx, r.Job); lerr == nil && portDeclined(log) {
				r.State, r.Handle = "unsupported", ""
				r.Detail = "the port declines to build on this platform"
				_ = prov.Release(context.WithoutCancel(ctx), r.Job)
			}
		case verify.Errored:
			r.State, r.Detail = "errored", st.Detail
			_ = prov.Release(context.WithoutCancel(ctx), r.Job)
		}
		n.Runs[plat], changed = r, true
	}
	if !changed {
		return nil
	}
	return writeNote(ctx, repo, *n)
}

// renderNote is the human rendering of a verdict set: one line per
// platform, in stable order.
func renderNote(n verifyNote) []string {
	var lines []string
	for _, plat := range n.platforms() {
		r := n.Runs[plat]
		s := r.State
		if r.State == "running" {
			s = fmt.Sprintf("verifying (%s)", time.Since(r.Job.Started).Round(time.Second))
		}
		line := fmt.Sprintf("%s (%s)", s, plat)
		if r.Handle != "" {
			line += " — environment kept: " + r.Handle
		}
		if r.Detail != "" {
			line += " — " + r.Detail
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return []string{"no runs recorded"}
	}
	return lines
}

// summarizeNote compresses a verdict set to one clause, for the
// drift lines.
func summarizeNote(n verifyNote) string {
	var parts []string
	for _, plat := range n.platforms() {
		parts = append(parts, n.Runs[plat].State+" ("+plat+")")
	}
	return strings.Join(parts, ", ")
}

// portDeclined reads a failure log for the shapes of a port refusing a
// platform rather than breaking on it. Conservative on purpose: an
// unrecognized refusal stays "failed", which is only ever a log-read
// away from the truth.
func portDeclined(log string) bool {
	for _, marker := range []string{"known to fail", "known_fail"} {
		if strings.Contains(log, marker) {
			return true
		}
	}
	return false
}

// describeUnverifiedTip says what an unnoted tip means: never
// verified, or verified at an older commit the branch has since moved
// past — the sha gap that IS the drift mechanism. Content identity is
// checked against every verdict, not just ancestors: an amend replaces
// the commit, so a reworded tip's verdicts live on a sha the branch no
// longer reaches, and the tree is what still matches.
func describeUnverifiedTip(ctx context.Context, repo *git.Repo, branch, tip string) (string, error) {
	tipTree, err := repo.RevParse(ctx, tip+"^{tree}")
	if err != nil {
		return "", err
	}
	noted, err := repo.NotesList(ctx, git.VerifyNotesRef)
	if err != nil {
		return "", err
	}
	for _, sha := range noted {
		n, err := readNote(ctx, repo, sha)
		if err != nil || n.Tree != tipTree || !n.anyState("passed") {
			continue
		}
		return fmt.Sprintf("%s at %s — the tip differs only in commit metadata", summarizeNote(n), sha[:12]), nil
	}
	shas, err := repo.RevList(ctx, branch, 32)
	if err != nil {
		return "", err
	}
	for behind, sha := range shas {
		if behind == 0 {
			continue
		}
		n, err := readNote(ctx, repo, sha)
		if err != nil {
			continue
		}
		return fmt.Sprintf("tip unverified; %s at %s, %d commit(s) behind — `dockhand verify %s` tests the tip",
			summarizeNote(n), sha[:12], behind, branch), nil
	}
	return "unverified", nil
}

// reportOrphanWorkers names running workers no note accounts for: a
// pre-mint gate failure keeps its environment with no branch, another
// checkout's jobs are invisible here, and with a two-guest cap a
// forgotten worker is an expensive kind of quiet. Best-effort — a
// machine without tart has no workers to report.
func reportOrphanWorkers(ctx context.Context, rs *runstate.Context, repo *git.Repo) {
	if !tartPresent() {
		return
	}
	out, err := tart.CLI(ctx, nil, "list", "--quiet")
	if err != nil {
		return
	}
	tracked := map[string]bool{}
	if noted, err := repo.NotesList(ctx, git.VerifyNotesRef); err == nil {
		for _, sha := range noted {
			if n, err := readNote(ctx, repo, sha); err == nil {
				for _, r := range n.Runs {
					tracked[r.Job.ID] = true
					tracked[r.Handle] = true
				}
			}
		}
	}
	for _, vm := range strings.Split(out, "\n") {
		vm = strings.TrimSpace(vm)
		if !strings.HasPrefix(vm, tart.WorkerPrefix) || tracked[vm] {
			continue
		}
		fmt.Fprintf(rs.Out, "%-32s untracked worker — `dockhand shell %s` reaches it; `tart delete %s` frees the slot\n", vm, vm, vm)
	}
}

// Status builds the status subcommand.
func Status() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report every dockhand branch and its verification standing",
		Args:  noArgs,
		RunE: runE(func(*cobra.Command, []string) (Action, error) {
			return statusAction{}, nil
		}),
	}
}
