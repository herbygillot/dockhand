package command

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
)

func cleanCommand(s *settings, streams Streams) *cobra.Command {
	var merged, yes bool
	cmd := &cobra.Command{
		Use:   "clean --merged",
		Short: "Remove what merged branches leave behind",
		Long: `Removes a merged branch's worktree, local branch, and your fork's branch,
each only while it still holds the merged commit. A worktree with edits or
untracked files, and work that went on past the merge, are kept. The
branch's record stays, so status --all still finds it.

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
			plans, err := e.PlanClean(ctx)
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
	cmd.Flags().BoolVar(&merged, "merged", true, "merged branches (the only kind clean removes)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "remove without asking")
	return cmd
}

// writeClean lists what clean would do, or did, and counts what it would
// remove.
func writeClean(out io.Writer, plans []engine.CleanBranch, done bool) int {
	count := 0
	for _, plan := range plans {
		line := plan.Branch.ShortName()
		if pr := plan.Branch.PullRequest; pr != nil {
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
