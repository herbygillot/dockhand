package tidy

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/spf13/cobra"
)

func RebaseCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchFlags
	var plan bool
	cmd := &cobra.Command{
		Use:   "rebase",
		Short: "Rebase the branch onto the latest master",
		Long: `Fetches MacPorts' master and replays the branch's commits on it, in the
branch's worktree, keeping the old history as a checkpoint that dockhand
undo brings back. A branch with uncommitted edits is refused, and a
rebase that conflicts is abandoned with the branch as it was.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			branch, err := where.Resolve(ctx, e, streams)
			if err != nil {
				return err
			}
			if plan {
				rebased, err := e.PlanRebase(ctx, branch)
				if err != nil {
					return err
				}
				writeRebasePlan(streams, branch, rebased)
				return nil
			}
			rebased, err := e.Rebase(ctx, branch)
			if err != nil {
				return err
			}
			result := map[string]any{"branch": branch.ShortName(), "from": rebased.From, "to": rebased.To, "up_to_date": rebased.UpToDate, "commits": rebased.Commits}
			if rebased.Checkpoint != nil {
				result["checkpoint"] = rebased.Checkpoint.Name()
			}
			streams.Emit(result)
			if rebased.UpToDate {
				fmt.Fprintf(streams.Out, "%s already starts from master %s (fetched just now).\n", branch.ShortName(), rebased.To.Short())
				return nil
			}
			name := rebased.Checkpoint.Name()
			fmt.Fprintf(streams.Out, "Rebased %s (%s) from master %s onto %s.\nCheckpoint %s keeps the old history (dockhand undo %s).\n",
				branch.ShortName(), prose.Plural(rebased.Commits, "commit"), rebased.From.Short(), rebased.To.Short(), name, name)
			if len(rebased.OlderBuilds) > 0 {
				fmt.Fprintf(streams.Out, "The rebased commits retain Generated-By values for older Dockhand builds: %s. A commit rewritten by tidy will name this build.\n", strings.Join(rebased.OlderBuilds, ", "))
			}
			if branch.PullRequest != nil {
				fmt.Fprintf(streams.Out, "Pull request #%d still has the old commits. dockhand submit will replace them only if no one else has pushed.\n", branch.PullRequest.Number)
			}
			// A check of the rebased files can already stand, as one does
			// after a second rebase onto the same master: status says what
			// then moves the branch on.
			moved, err := e.Branch(ctx, branch.ID)
			if err != nil {
				return err
			}
			status, err := e.BranchStatus(ctx, moved)
			if err != nil {
				return err
			}
			if status.Current {
				fmt.Fprintf(streams.Out, "%s checked these files already: %s.\n", status.Latest.Name(), view.CheckState(status))
				view.WriteNext(streams.Out, status)
				return nil
			}
			fmt.Fprintln(streams.Out, "Next: "+branchflags.NextIn(ctx, e, branch, "dockhand check (the files it builds on have changed)"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&plan, "plan", false, "show what the rebase would replay, and onto what, and change nothing")
	where.Register(cmd, s, "rebase")
	settings.SupportsJSON(cmd)
	return cmd
}

// writeRebasePlan says what a rebase would do with each of the branch's
// commits, or what it would refuse, having moved nothing.
func writeRebasePlan(streams term.Streams, branch model.Branch, rebased engine.Rebased) {
	replay := []map[string]any{}
	for _, made := range rebased.Replay {
		commit := map[string]any{"id": made.Commit.ID, "subject": made.Commit.Subject(), "outcome": made.Outcome}
		if len(made.Paths) > 0 {
			commit["paths"] = made.Paths
		}
		replay = append(replay, commit)
	}
	result := map[string]any{"branch": branch.ShortName(), "from": rebased.From, "to": rebased.To, "up_to_date": rebased.UpToDate, "commits": rebased.Commits, "plan": true, "replay": replay}
	if rebased.Refused != "" {
		result["refused"] = rebased.Refused
	}
	streams.Emit(result)
	name := branch.ShortName()
	switch {
	case rebased.UpToDate:
		fmt.Fprintf(streams.Out, "%s already starts from master %s (fetched just now).\n", name, rebased.To.Short())
		return
	case len(rebased.Replay) == 0:
		fmt.Fprintf(streams.Out, "%s.\nNothing moved.\n", rebased.Refused)
		return
	}
	fmt.Fprintf(streams.Out, "Rebase %s (%s) from master %s onto %s (fetched just now):\n", name, prose.Plural(len(rebased.Replay), "commit"), rebased.From.Short(), rebased.To.Short())
	conflict := false
	for _, made := range rebased.Replay {
		short := model.ObjectID(made.Commit.ID).Short()
		switch made.Outcome {
		case engine.ReplayDropped:
			fmt.Fprintf(streams.Out, "  drop     %s %s (master %s has this change)\n", short, made.Commit.Subject(), rebased.To.Short())
		case engine.ReplayConflict:
			conflict = true
			fmt.Fprintf(streams.Out, "  conflict %s %s, in %s\n", short, made.Commit.Subject(), strings.Join(made.Paths, ", "))
		case engine.ReplayUntried:
			fmt.Fprintf(streams.Out, "  untried  %s %s\n", short, made.Commit.Subject())
		default:
			fmt.Fprintf(streams.Out, "  replay   %s %s\n", short, made.Commit.Subject())
		}
	}
	switch {
	case conflict:
		fmt.Fprintf(streams.Out, "dockhand rebase would stop here, with %s as it is; resolve it by hand with git rebase %s, or ask for help on the PR.\n", name, rebased.To.Short())
		return
	case rebased.Refused != "":
		fmt.Fprintf(streams.Out, "%s; dockhand clean %s removes it once its pull request is merged.\nNothing moved.\n", rebased.Refused, name)
		return
	}
	if len(rebased.OlderBuilds) > 0 {
		fmt.Fprintf(streams.Out, "The rebased commits would retain Generated-By values for older Dockhand builds: %s.\n", strings.Join(rebased.OlderBuilds, ", "))
	}
	if branch.PullRequest != nil {
		fmt.Fprintf(streams.Out, "Pull request #%d would still have the old commits, which dockhand submit replaces only if no one else has pushed.\n", branch.PullRequest.Number)
	}
	fmt.Fprintln(streams.Out, "Nothing moved. dockhand rebase replays them and keeps a checkpoint.")
}
