package tidy

import (
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/spf13/cobra"
)

func RebaseCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var where branchflags.BranchFlags
	cmd := &cobra.Command{
		Use:   "rebase",
		Short: "Rebase the branch onto the latest master",
		Long: `Fetches MacPorts' master and replays the branch's commits on it, in the
branch's worktree, keeping the old history as a checkpoint that dockhand
restore brings back. A branch with uncommitted edits is refused, and a
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
	where.Register(cmd, s, "rebase")
	settings.SupportsJSON(cmd)
	return cmd
}
