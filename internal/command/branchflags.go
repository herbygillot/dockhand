package command

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
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
func (b branchFlags) resolve(ctx context.Context, e *engine.Engine, streams Streams) (model.Branch, error) {
	var branch model.Branch
	var err error
	switch {
	case b.branch != "":
		branch, err = e.Resolve(ctx, b.branch)
	case b.pr != 0:
		branch, err = e.PullRequestBranch(ctx, b.pr)
	case b.port != "":
		branch, err = portBranch(ctx, e, streams, b.port)
	default:
		branch, err = e.Current(ctx)
		// On master, the engine's refusal already says how to name one.
		if errors.Is(err, engine.ErrNoBranch) && !errors.Is(err, engine.ErrYourCheckout) {
			return model.Branch{}, fmt.Errorf("%w; name one with -b <branch> or -p <port>, or run this in the branch's worktree (dockhand path <branch>)", err)
		}
		return branch, err
	}
	if err != nil {
		return model.Branch{}, err
	}
	if here, err := e.Current(ctx); err == nil && here.ID != branch.ID {
		fmt.Fprintf(streams.Err, "%s (not %s, checked out here)\n", branch.ShortName(), here.ShortName())
	}
	return branch, nil
}

// portBranch is the open branch that changes a port, as -p names it.
func portBranch(ctx context.Context, e *engine.Engine, streams Streams, port string) (model.Branch, error) {
	found, err := e.PortBranches(ctx, port)
	switch {
	case err != nil:
		return model.Branch{}, err
	case len(found) == 0:
		return model.Branch{}, fmt.Errorf("%w changes %s; dockhand status --port %s lists every branch touching it", engine.ErrNoBranch, port, port)
	case len(found) == 1:
		fmt.Fprintf(streams.Err, "Working in %s, the one open branch changing %s.\n", found[0].Branch.ShortName(), port)
		return found[0].Branch, nil
	case !streams.terminal():
		return model.Branch{}, &engine.AmbiguousError{Port: port, Branches: found}
	}
	fmt.Fprintf(streams.Err, "%s is changed in %d open branches:\n", port, len(found))
	for i, f := range found {
		fmt.Fprintf(streams.Err, "  %d. %s\n", i+1, branchPurpose(f))
	}
	answer, err := ask(streams, fmt.Sprintf("? which one? [1-%d] ", len(found)))
	if err != nil {
		return model.Branch{}, err
	}
	n, err := strconv.Atoi(answer)
	if err != nil || n < 1 || n > len(found) {
		return model.Branch{}, errors.New("nothing chosen, so nothing changed")
	}
	return found[n-1].Branch, nil
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
