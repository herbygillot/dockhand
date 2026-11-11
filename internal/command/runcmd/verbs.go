package runcmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/checks"
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
		Long: `Repeats a finished check, such as check-42, using its original files,
plan, and environments, even if the branch has changed. Use check to
capture newer edits instead. Reuses earlier results whose inputs still
match, unless the original check used --fresh.

With no check named in a branch's worktree, repeats that branch's latest
check. Use -b, -p, or --pr to select a branch from another directory.

With --ci, reruns the branch's failed MacPorts pull request CI jobs instead.
Your GitHub account needs write access to MacPorts' repository. This is
useful for temporary failures, such as a mirror refusing an archive.`,
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
			fmt.Fprintf(streams.Prose(), "%s repeats %s: %s\n", run.Name(), previous.Name(), checks.Describe(revision))
			return checkrun.RunQueued(ctx, e, run, streams, enqueue, s.Places(), s.Drain)
		},
	}
	cmd.Flags().BoolVarP(&enqueue, "enqueue", "d", false, "queue it and return")
	cmd.Flags().BoolVar(&ci, "ci", false, "run the branch's pull request's failed MacPorts CI jobs again, through your GitHub login")
	where.Register(cmd, s, "repeat the latest check of")
	// --ci queues nothing here, so -d had nothing to queue and was
	// ignored (Codex's workflow review of 1caa6c3d).
	cmd.MarkFlagsMutuallyExclusive("ci", "enqueue")
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
	fmt.Fprintf(streams.Prose(), "Asked GitHub to run #%d's failed CI again (%s); dockhand status --refresh reads how it went.\n", number, prose.Plural(len(runs), "workflow run"))
	return nil
}
