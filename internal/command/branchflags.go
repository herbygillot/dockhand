package command

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
)

// branchFlags are how a command about a whole branch is told which (the
// command-line UX review's §1, revised): -b its exact name, -p the port
// it changes, or --pr the pull request it tracks; with none of them, the
// branch checked out here. Each flag has one meaning, so a port's name
// and a branch's never collide, and none is a positional argument.
type branchFlags struct {
	branch, port string
	pr           int
}

// register adds the flags to a command, -b with completion of the open
// branches' names.
func (b *branchFlags) register(cmd *cobra.Command, s *settings, verb string) {
	cmd.Flags().StringVarP(&b.branch, "branch", "b", "", verb+" this branch, by its exact name (the dockhand/ prefix is optional)")
	cmd.Flags().StringVarP(&b.port, "port", "p", "", verb+" the open branch that changes this port")
	cmd.Flags().IntVar(&b.pr, "pr", 0, verb+" the branch tracking this pull request, by number")
	cmd.MarkFlagsMutuallyExclusive("branch", "port", "pr")
	_ = cmd.RegisterFlagCompletionFunc("branch", branchCompletion(s))
}

// explicit says whether a flag named the branch, rather than where the
// command runs.
func (b branchFlags) explicit() bool {
	return b.branch != "" || b.port != "" || b.pr != 0
}

// resolve finds the branch the flags name, else the one checked out here.
// A port one open branch changes names it, said on the first line; one
// several change is asked about on a terminal and refused in a script,
// with their exact names. A flag beats where the command runs, and says
// so where the two differ.
func (b branchFlags) resolve(ctx context.Context, e *engine.Engine, streams term.Streams) (model.Branch, error) {
	selection, err := e.Select(ctx, engine.Selector{Branch: b.branch, PR: b.pr, Port: b.port, Policy: engine.SelectExisting}, engine.WorkNone)
	// A checkout dockhand doesn't track is offered what tracks it, on a
	// terminal, where nothing else could be meant; a script is refused,
	// naming it (the roadmap's part 3, by the command-line UX review's §2).
	if !b.explicit() {
		if branch, offered, offerErr := offerToTrack(ctx, e, streams, err); offered {
			return branch, offerErr
		}
	}
	if err != nil {
		// On master, the engine's refusal already says how to name one.
		if !b.explicit() && errors.Is(err, engine.ErrNoBranch) && !errors.Is(err, engine.ErrYourCheckout) {
			return model.Branch{}, fmt.Errorf("%w; name one with -b <branch> or -p <port>, or run this in the branch's worktree (dockhand path <branch>)", err)
		}
		return model.Branch{}, err
	}
	chosen, err := settleSelection(ctx, e, streams, selection, b.port, engine.WorkNone, false)
	branch := chosen.branch
	if err != nil || !b.explicit() {
		return branch, err
	}
	if here, err := e.Current(ctx); err == nil && here.ID != branch.ID {
		fmt.Fprintf(streams.Err, "%s (not %s, checked out here)\n", branch.ShortName(), here.ShortName())
	}
	return branch, nil
}

// branchPurpose is a branch as a choice among several says it: its name,
// its title or pull request, and whether it changes the port's revision
// only.
func branchPurpose(f engine.PortBranch) string {
	words := []string{f.Branch.ShortName()}
	if pr := f.Branch.PullRequest; pr != nil {
		words = append(words, fmt.Sprintf("#%d", pr.Number))
	}
	if f.Branch.Title != "" {
		words = append(words, f.Branch.Title)
	}
	if f.RevisionOnly {
		words = append(words, "(revision only)")
	}
	return strings.Join(words, "  ")
}

// branchCompletion completes -b with the open branches' names, each
// described by its pull request and title where it has them.
func branchCompletion(s *settings) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		e, err := s.open(cmd.Context())
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		defer e.Close()
		open, err := e.OpenBranches(cmd.Context())
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var completions []cobra.Completion
		for _, branch := range open {
			if !strings.HasPrefix(branch.ShortName(), toComplete) {
				continue
			}
			describe := branchPurpose(engine.PortBranch{Branch: branch})
			describe = strings.TrimSpace(strings.TrimPrefix(describe, branch.ShortName()))
			completions = append(completions, cobra.CompletionWithDesc(branch.ShortName(), describe))
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

// offerToTrack asks, on a terminal, to track the checkout a command found
// untracked: a Git branch someone made, adopted, and a detached HEAD, a
// branch started there for its port. offered is false where err is
// neither, or nothing was asked.
func offerToTrack(ctx context.Context, e *engine.Engine, streams term.Streams, err error) (model.Branch, bool, error) {
	untracked, detached := new(engine.UntrackedError), new(engine.DetachedError)
	switch {
	case errors.As(err, &untracked):
		ok, _, askErr := streams.Decide(false, term.Decision{Question: fmt.Sprintf("? %s isn't tracked; track it with adopt? [y/N] ", untracked.Name), Undone: "nothing was done",
			ByYes: "dockhand adopt " + untracked.Name + " tracks it, then run this again"})
		if askErr != nil || !ok {
			return model.Branch{}, true, cmp.Or(askErr, fmt.Errorf("nothing was done: %s isn't tracked; dockhand adopt %s tracks it", untracked.Name, untracked.Name))
		}
		adopted, err := e.Adopt(ctx, engine.AdoptRequest{Branch: untracked.Name})
		if err == nil {
			fmt.Fprintf(streams.Err, "Tracking %s.\n", adopted.Branch.ShortName())
		}
		return adopted.Branch, true, err
	case errors.As(err, &detached):
		ok, _, askErr := streams.Decide(false, term.Decision{Question: "? this checkout is at a commit, on no branch; start one here for the port it changes? [y/N] ", Undone: "nothing was done",
			ByYes: "on a terminal it starts one here; or git switch -c dockhand/<name> and dockhand adopt"})
		if askErr != nil || !ok {
			return model.Branch{}, true, cmp.Or(askErr, errors.New("nothing was done: this checkout is on no branch; git switch -c dockhand/<name> and dockhand adopt track one"))
		}
		adopted, err := e.StartAtHead(ctx)
		if err == nil {
			fmt.Fprintf(streams.Err, "Started %s at this commit, and tracking it.\n", adopted.Branch.ShortName())
		}
		return adopted.Branch, true, err
	}
	return model.Branch{}, false, nil
}
