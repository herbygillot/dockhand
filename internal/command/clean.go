package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/config"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

func cleanCommand(s *settings, streams Streams) *cobra.Command {
	var merged, closed, archived, yes, automatic bool
	cmd := &cobra.Command{
		Use:   "clean [--merged] [--closed] [--archived]",
		Short: "Remove what merged branches leave behind",
		Long: `Removes a merged branch's worktree, local branch, and your fork's branch,
each only while it still holds the merged commit. A worktree with edits or
untracked files, and work that went on past the merge, are kept. The
branch's record stays: status --all lists it as cleaned, and status
<branch> still finds it.

--closed and --archived take the worktrees of branches whose pull request
was closed without merging, or that were archived, and nothing else: their
work isn't merged, so the Git branch, your fork's branch, and the
checkpoints stay, and dockhand path or any command that needs the worktree
checks it out again. A worktree with edits or untracked files is kept.

Whichever branches it cleans, it also removes what checks left in their
providers when the process running them died: a Tart clone, which a check
deletes when it ends, and a later attempt of the same check when it starts.
One is removed only when a check of this checkout made it and no process
is running that check; one no check of this checkout made is kept, since
another database may be using it.

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
			if automatic {
				return cleanAutomatically(ctx, e, streams, s.file)
			}
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
			session, err := startSession(ctx, e, model.SessionForeground)
			if err != nil {
				return err
			}
			defer session.End(context.WithoutCancel(ctx))
			leftovers, err := e.PlanLeftovers(ctx, session)
			if err != nil {
				return err
			}
			streams.emit(map[string]any{"branches": cleanView(plans), "leftovers": leftoversView(leftovers), "applied": false})
			removable := writeClean(streams.Out, plans, false) + writeLeftovers(streams.Out, leftovers, false)
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
			removed, leftoverErr := e.RemoveLeftovers(ctx, session, leftovers)
			streams.emit(map[string]any{"branches": cleanView(done), "leftovers": leftoversView(removed), "applied": true})
			fmt.Fprintln(streams.Out)
			writeClean(streams.Out, done, true)
			writeLeftovers(streams.Out, removed, true)
			return errors.Join(err, leftoverErr)
		},
	}
	cmd.Flags().BoolVar(&merged, "merged", true, "merged branches: their worktree, local branch, and fork branch")
	cmd.Flags().BoolVar(&closed, "closed", false, "branches whose pull request closed unmerged: their worktree only")
	cmd.Flags().BoolVar(&archived, "archived", false, "archived branches: their worktree only")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "remove without asking")
	cmd.Flags().BoolVar(&automatic, "automatic", false, "run automatic cleanup's pass, when it is due, as a command starts it once its work is done")
	_ = cmd.Flags().MarkHidden("automatic")
	return cmd
}

// cleanAutomatically is decision 36's automatic pass, as a command starts it
// apart from itself once its own work is done: it runs when still due,
// since another process may have run it meanwhile, stamping first so a
// pass that fails isn't tried again at once, and says what it removed.
func cleanAutomatically(ctx context.Context, e *engine.Engine, streams Streams, file config.File) error {
	due, why := e.CleanupDue(engine.CleanupEvery, file.Cleanup.Free())
	if !due {
		return nil
	}
	if err := e.StampCleanup(); err != nil {
		return err
	}
	session, err := startSession(ctx, e, model.SessionForeground)
	if err != nil {
		return err
	}
	defer session.End(context.WithoutCancel(ctx))
	fmt.Fprintf(streams.Out, "%s cleaning up: %s\n", time.Now().Format(time.RFC3339), why.Words)
	report, err := e.Cleanup(ctx, session, file.Cleanup.Age())
	for _, name := range report.Caches {
		fmt.Fprintf(streams.Out, "  removed %s from Tart's cache, unused for %s\n", name, engine.CacheUnused)
	}
	fmt.Fprintf(streams.Out, "  removed %s\n", plural(report.Removed(), "item"))
	return err
}

// cleanupAfter starts automatic cleanup apart from this process once a
// command's own work is done, when it is due (decision 36): a day since the
// last, or free space short, which it says. The command doesn't wait for
// it. serve cleans up itself, and clean is cleaning.
func (s *settings) cleanupAfter(streams Streams, command string) {
	e := s.opened
	if e == nil || !s.file.Cleanup.On() || command == "clean" || command == "serve" {
		return
	}
	due, why := e.CleanupDue(engine.CleanupEvery, s.file.Cleanup.Free())
	if !due {
		return
	}
	if why.LowSpace {
		fmt.Fprintf(streams.Err, "Cleaning up in the background: %s.\n", why.Words)
	}
	if err := startCleanup(s.openedWith); err != nil {
		fmt.Fprintf(streams.Err, "Couldn't start cleaning up in the background: %v\n", err)
	}
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

// writeLeftovers lists what checks left in providers, what clean does with
// each, or did, and counts what it would remove.
func writeLeftovers(out io.Writer, leftovers []engine.Leftover, done bool) int {
	if len(leftovers) == 0 {
		return 0
	}
	fmt.Fprintln(out, "Left by checks")
	count := 0
	for _, leftover := range leftovers {
		switch {
		case leftover.Kept != "":
			fmt.Fprintf(out, "  keep     %s: %s\n", leftover.What, leftover.Kept)
		case done && leftover.Done:
			fmt.Fprintf(out, "  removed  %s, left by %s\n", leftover.What, leftover.Run.Name())
		default:
			fmt.Fprintf(out, "  remove   %s, left by %s\n", leftover.What, leftover.Run.Name())
			count++
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
