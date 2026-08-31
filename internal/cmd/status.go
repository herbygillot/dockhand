package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
)

// statusAction reconciles the dockhand/* namespace: every branch, its
// tip's verification record, and the drift between them. It is a
// reconciler, not a daemon (D21): running jobs are polled here, their
// verdicts written back to the notes, and workers released on pass —
// the one mutation status performs, because a two-slot provider's
// unreleased job is a slot that never returns. It never deletes a
// branch: cleanup is the user's explicit act.
type statusAction struct{}

var _ Action = statusAction{}

func (statusAction) Execute(ctx context.Context, rs *runstate.Context) error {
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
		// Naming the repository is the point: run from the wrong
		// checkout, "no branches" is true and useless — true and
		// located is actionable.
		fmt.Fprintf(rs.Out, "no dockhand branches in %s\n", repo.Root)
		return nil
	}
	for _, br := range branches {
		line, err := describeBranch(ctx, repo, br)
		if err != nil {
			line = "error: " + err.Error()
		}
		fmt.Fprintf(rs.Out, "%-32s %s\n", br, line)
	}
	return nil
}

// describeBranch renders one branch's verification standing, polling
// and settling a running job when the tip carries one.
func describeBranch(ctx context.Context, repo *git.Repo, branch string) (string, error) {
	tip, err := repo.RevParse(ctx, branch)
	if err != nil {
		return "", err
	}
	n, err := readNote(ctx, repo, tip)
	if errors.Is(err, git.ErrNoNote) {
		return describeUnverifiedTip(ctx, repo, branch, tip)
	}
	if err != nil {
		return "", err
	}
	if n.State == "running" {
		return settleRunning(ctx, repo, &n)
	}
	return renderState(n), nil
}

// settleRunning polls the tip's job and writes what it learns back to
// the note. Poll never mutates and Release is the caller's (D17):
// status releases the worker on pass — a kept green environment is a
// wasted slot — and keeps it on failure, where it is the debug handle.
func settleRunning(ctx context.Context, repo *git.Repo, n *verifyNote) (string, error) {
	prov, err := vmProvider(ctx)
	if err != nil {
		return fmt.Sprintf("running, cannot poll: %v", err), nil
	}
	st, err := prov.Poll(ctx, n.Job)
	if errors.Is(err, verify.ErrUnknownJob) {
		n.State, n.Detail = "errored", "job vanished: its worker no longer exists"
		if werr := writeNote(ctx, repo, *n); werr != nil {
			return "", werr
		}
		return renderState(*n), nil
	}
	if err != nil {
		return "", err
	}
	switch st.State {
	case verify.Running:
		return fmt.Sprintf("verifying (%s)", time.Since(n.Job.Started).Round(time.Second)), nil
	case verify.Passed:
		n.State = "passed"
		if rerr := prov.Release(ctx, n.Job); rerr != nil {
			n.Detail = "worker not released: " + rerr.Error()
		}
	case verify.Failed:
		n.State, n.Handle = "failed", st.Handle
	case verify.Errored:
		n.State, n.Detail = "errored", st.Detail
		_ = prov.Release(context.WithoutCancel(ctx), n.Job)
	}
	if err := writeNote(ctx, repo, *n); err != nil {
		return "", err
	}
	return renderState(*n), nil
}

// describeUnverifiedTip says what an unnoted tip means: never
// verified, or verified at an older commit the branch has since moved
// past — the sha gap that IS the drift mechanism (D21). Content
// identity is checked against every verdict, not just ancestors: an
// amend replaces the commit, so a reworded tip's passed verdict lives
// on a sha the branch no longer reaches, and the tree is what still
// matches.
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
		if err != nil || n.State != "passed" || n.Tree != tipTree {
			continue
		}
		return fmt.Sprintf("passed for identical content at %s — the tip differs only in commit metadata", sha[:12]), nil
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
		verb := n.State
		if n.State == "running" {
			verb = "verifying"
		}
		return fmt.Sprintf("tip unverified; %s at %s, %d commit(s) behind — `dockhand verify %s` tests the tip", verb, sha[:12], behind, branch), nil
	}
	return "unverified", nil
}

// renderState is the terse rendering of a settled note.
func renderState(n verifyNote) string {
	s := n.State
	if n.Platform != "" {
		s += " (" + n.Platform + ")"
	}
	if n.Handle != "" {
		s += " — environment kept: " + n.Handle
	}
	if n.Detail != "" {
		s += " — " + n.Detail
	}
	return s
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
