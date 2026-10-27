package clean

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/prose"
	"github.com/herbygillot/dockhand/internal/term"
	"github.com/spf13/cobra"
)

func ArchiveCommand(s *settings.Settings, streams term.Streams) *cobra.Command {
	var undo, keep, discard bool
	var where branchflags.BranchFlags
	cmd := &cobra.Command{
		Use:   "archive [branch]",
		Short: "Set aside a branch you are not working on",
		Long: `Sets a branch aside: status leaves it out, and its worktree is removed
where it holds nothing the branch's commits don't. The Git branch, your
fork's branch, the checkpoints, the record, and the pull request stay, so
--undo brings it back, and the worktree is checked out again when a command
next needs it; status --all still shows it.

A worktree with uncommitted edits or untracked files is asked about on a
terminal: commit them, as tidy would, where its plan needs no words from
you; discard them; or keep the worktree. A script keeps it and says so;
--discard removes it, edits and all, and --keep-worktree keeps any worktree.
A pull request still open is said, with how to close it: archive never
closes it.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			defer e.Close()
			if len(args) == 1 {
				if where.Explicit() {
					return errors.New("name the branch once: as the argument, or with -b, -p, or --pr")
				}
				where.Branch = args[0]
			}
			branch, err := where.Resolve(ctx, e, streams)
			if err != nil {
				return err
			}
			request := engine.ArchiveRequest{Branch: branch, Undo: undo, KeepWorktree: keep, Discard: discard}
			archived, err := e.ArchiveBranch(ctx, request)
			if err != nil {
				return err
			}
			if archived.Kept != "" && streams.Terminal() {
				if archived, err = commitOrDiscard(ctx, e, streams, request, archived); err != nil {
					return err
				}
			}
			branch = archived.Branch
			streams.Emit(map[string]any{"branch": branch.ShortName(), "state": branch.State, "worktree_removed": archived.Removed != "", "worktree_kept": archived.Kept,
				"ignored_removed": view.NonNil(archived.IgnoredRemoved)})
			out := streams.Out
			if undo {
				fmt.Fprintf(out, "%s is back among your open branches.\n", branch.ShortName())
				return nil
			}
			switch {
			case archived.Removed != "":
				fmt.Fprintf(out, "Archived %s and removed its worktree. The Git branch and pull request remain. Run dockhand archive --undo %s to restore the archived branch.\n", branch.ShortName(), branch.ShortName())
				// What Git ignores goes with the worktree, and no branch
				// keeps it: said, as nothing else lists it.
				if ignored := archived.IgnoredRemoved; len(ignored) > 0 {
					fmt.Fprintf(out, "Its ignored files went with it: %s\n", prose.Few(ignored, 5, "file"))
				}
			case archived.Kept != "":
				fmt.Fprintf(out, "Archived %s; its worktree was kept because %s. dockhand archive --discard %s removes the worktree and its edits.\n", branch.ShortName(), archived.Kept, branch.ShortName())
			default:
				fmt.Fprintf(out, "Archived %s; its files, Git branch, and pull request are unchanged. Run dockhand archive --undo %s to restore the archived branch.\n", branch.ShortName(), branch.ShortName())
			}
			if pr := branch.PullRequest; pr != nil && archived.PullRequestOpen {
				fmt.Fprintf(out, "Pull request #%d is still open; archiving does not close it. Close it on GitHub or run: gh pr close %d --repo %s\n", pr.Number, pr.Number, pr.Repository)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&undo, "undo", false, "bring an archived branch back")
	cmd.Flags().BoolVar(&keep, "keep-worktree", false, "keep the worktree")
	cmd.Flags().BoolVar(&discard, "discard", false, "remove the worktree, including any uncommitted edits and untracked files")
	cmd.MarkFlagsMutuallyExclusive("keep-worktree", "discard")
	where.Register(cmd, s, "archive")
	settings.SupportsJSON(cmd)
	return cmd
}

// commitOrDiscard asks what to do with the edits a worktree being
// archived holds: commit them, where tidy's plan needs no words from the
// person; discard them; or keep the worktree.
func commitOrDiscard(ctx context.Context, e *engine.Engine, streams term.Streams, request engine.ArchiveRequest, archived engine.Archived) (engine.Archived, error) {
	fmt.Fprintf(streams.Err, "%s's worktree stays for now: %s.\n", archived.Branch.ShortName(), archived.Kept)
	answer, err := streams.Ask("? commit them, discard them, or keep the worktree? [c]ommit / [d]iscard / [K]eep ")
	if err != nil {
		return archived, err
	}
	request.Branch = archived.Branch
	switch strings.ToLower(answer) {
	case "c", "commit":
		request.Commit = true
		again, err := e.ArchiveBranch(ctx, request)
		if err == nil && again.NeedsWords {
			fmt.Fprintf(streams.Err, "Its commits need your words; dockhand tidy -b %s shapes them, and archive takes the worktree after.\n", archived.Branch.ShortName())
			return archived, nil
		}
		return again, err
	case "d", "discard":
		request.Discard = true
		return e.ArchiveBranch(ctx, request)
	}
	return archived, nil
}
