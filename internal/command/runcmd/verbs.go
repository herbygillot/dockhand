package runcmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/checkrun"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
)

func RetryCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var enqueue, ci bool
	var where branchflags.BranchFlags
	cmd := &cobra.Command{
		Use:   "retry [check]",
		Short: "Repeat a finished check with the same files and plan",
		Long: `Queues a finished check again, such as check-42: the same files, the same
plan, and the same environments, whatever the branch holds now. check
captures newer edits instead. Until results are reused port by port, the
whole plan is built again. In a branch's worktree, with no check named, the
branch's latest; -b, -p, or --pr names a branch, for its latest check from
anywhere.

With --ci, it runs the branch's pull request's failed MacPorts CI jobs
again instead, through your GitHub login, which needs write access to
MacPorts' repository, as a maintainer has: for a failure another run may
get past, as a mirror refusing an archive.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			if ci {
				if len(args) > 0 {
					return fmt.Errorf("--ci runs a pull request's CI again, not a check: name its branch with -b, -p, or --pr")
				}
				return retryCI(ctx, e, streams, where)
			}
			previous, err := branchflags.NamedOrLatest(ctx, e, streams, where, args, "name a check, such as check-42, or a branch with -b or -p; in a branch's worktree, retry repeats the branch's latest check")
			if err != nil {
				return err
			}
			run, err := e.Retry(ctx, previous)
			if err != nil {
				return err
			}
			revision, err := e.Revision(ctx, run.Revision)
			if err != nil {
				return err
			}
			fmt.Fprintf(streams.Out, "%s repeats %s: %s\n", run.Name(), previous.Name(), engine.Describe(revision))
			return checkrun.RunQueued(ctx, e, run, streams, enqueue, s.Places(), s.Drain)
		},
	}
	cmd.Flags().BoolVarP(&enqueue, "enqueue", "d", false, "queue it and return")
	cmd.Flags().BoolVar(&ci, "ci", false, "run the branch's pull request's failed MacPorts CI jobs again, through your GitHub login")
	where.Register(cmd, s, "repeat the latest check of")
	settings.SupportsJSON(cmd)
	return cmd
}

// retryCIJSON is what retry --ci asked of GitHub: the pull request, and
// the workflow runs whose failed jobs run again.
type retryCIJSON struct {
	PullRequest int     `json:"pull_request"`
	Runs        []int64 `json:"runs"`
}

// retryCI runs a branch's pull request's failed CI jobs again, as status's
// Next for a CI failure another run may get past names it.
func retryCI(ctx context.Context, e *engine.Engine, streams term.Streams, where branchflags.BranchFlags) error {
	branch, err := where.Resolve(ctx, e, streams)
	if err != nil {
		return err
	}
	runs, err := e.RerunCI(ctx, branch)
	if err != nil {
		return err
	}
	number := branch.PullRequest.Number
	streams.Emit(retryCIJSON{PullRequest: number, Runs: runs})
	fmt.Fprintf(streams.Out, "Asked GitHub to run #%d's failed CI again (%s); dockhand status --refresh reads how it went.\n", number, prose.Plural(len(runs), "workflow run"))
	return nil
}
