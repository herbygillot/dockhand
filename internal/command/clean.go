package command

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

func cleanCommand(s *settings, streams Streams) *cobra.Command {
	var merged, closed, archived, yes bool
	cmd := &cobra.Command{
		Use:   "clean [--merged] [--closed] [--archived]",
		Short: "Remove what merged branches leave behind",
		Long: `Removes a merged branch's worktree, local branch, and your fork's branch,
each only while it still holds the merged commit. A worktree with edits or
untracked files, and work that went on past the merge, are kept. The
branch's record stays, so status --all still finds it.

--closed and --archived take the worktrees of branches whose pull request
was closed without merging, or that were archived, and nothing else: their
work isn't merged, so the Git branch, your fork's branch, and the
checkpoints stay, and dockhand path or any command that needs the worktree
checks it out again. A worktree with edits or untracked files is kept.

It shows what it would remove first; on a terminal it asks, and a script
passes --yes. archive hides a branch; clean removes a merged one's files;
cancel stops a check. None means another.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			e, err := s.open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			var states []model.BranchState
			if merged && (cmd.Flags().Changed("merged") || !closed && !archived) {
				states = append(states, model.BranchMerged)
			}
			if closed {
				states = append(states, model.BranchClosed)
			}
			if archived {
				states = append(states, model.BranchArchived)
			}
			if len(states) == 0 {
				return errors.New("nothing to clean: --merged, --closed, or --archived names what")
			}
			plans, err := e.PlanClean(ctx, states...)
			if err != nil {
				return err
			}
			streams.emit(map[string]any{"branches": cleanView(plans), "applied": false})
			removable := writeClean(streams.Out, plans, false)
			if removable == 0 {
				fmt.Fprintln(streams.Out, "Nothing to remove.")
				return nil
			}
			if !yes {
				if !streams.terminal() {
					fmt.Fprintln(streams.Out, "Nothing was removed; --yes removes these.")
					return nil
				}
				ok, err := confirm(streams, fmt.Sprintf("? Remove %s? [y/N] ", plural(removable, "item")))
				if err != nil || !ok {
					fmt.Fprintln(streams.Out, "Nothing was removed.")
					return err
				}
			}
			done, err := e.ApplyClean(ctx, plans)
			streams.emit(map[string]any{"branches": cleanView(done), "applied": true})
			fmt.Fprintln(streams.Out)
			writeClean(streams.Out, done, true)
			return err
		},
	}
	cmd.Flags().BoolVar(&merged, "merged", true, "merged branches: their worktree, local branch, and fork branch")
	cmd.Flags().BoolVar(&closed, "closed", false, "branches whose pull request closed unmerged: their worktree only")
	cmd.Flags().BoolVar(&archived, "archived", false, "archived branches: their worktree only")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "remove without asking")
	return cmd
}

// writeClean lists what clean would do, or did, and counts what it would
// remove.
func writeClean(out io.Writer, plans []engine.CleanBranch, done bool) int {
	count := 0
	for _, plan := range plans {
		line := plan.Branch.ShortName()
		pr := plan.Branch.PullRequest
		switch {
		case plan.Branch.State == model.BranchArchived:
			line += " (archived)"
		case pr != nil && plan.Branch.State == model.BranchClosed:
			line += fmt.Sprintf(" (#%d, closed unmerged)", pr.Number)
		case pr != nil:
			line += fmt.Sprintf(" (#%d, merged at %s)", pr.Number, engine.Short(plan.Merged))
		}
		fmt.Fprintln(out, line)
		for _, step := range plan.Steps {
			switch {
			case step.Kept != "":
				fmt.Fprintf(out, "  keep     %s: %s\n", cleanWords(step.What), step.Kept)
			case done && step.Done:
				fmt.Fprintf(out, "  removed  %s\n", cleanWords(step.What))
			default:
				fmt.Fprintf(out, "  remove   %s\n", cleanWords(step.What))
				count++
			}
		}
		if plan.Branch.State != model.BranchMerged && slices.ContainsFunc(plan.Steps, func(s engine.CleanStep) bool { return s.Kept == "" }) {
			fmt.Fprintf(out, "  keep     branch %s: dockhand path %s checks it out again\n", plan.Branch.Name, plan.Branch.ShortName())
		}
	}
	return count
}

// cleanWords abbreviates a worktree's path under your home.
func cleanWords(what string) string {
	if path, ok := strings.CutPrefix(what, "worktree "); ok {
		return "worktree " + tilde(path)
	}
	return what
}
