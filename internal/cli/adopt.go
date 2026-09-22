package cli

import (
	"fmt"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/version"
	"github.com/spf13/cobra"
)

func (r *runtime) adoptCommand() *cobra.Command {
	var dryRun, squash, keepBody bool
	var pullRequest string
	command := &cobra.Command{
		Use: "adopt <branch> [port]", Short: "Track a branch, or a pull request, you did not make with dockhand",
		Long:    "Adopt a branch dockhand did not make: one commit above a commit of MacPorts master, changing one port directory, with shared files under _resources allowed beside it, whose port is inferred from that directory unless named. Once tracked, every verb selects it by port name: verify builds it, publish opens its pull request, amend and rebase revise it, and status shows it. This is the way in for a Portfile dockhand cannot edit itself, and for a new port. The branch stays where it is under its own name. A branch with several commits is refused unless --squash folds them into one, keeping the originals under a backup ref; changes in several port directories are refused and named. --dry-run reports what would be tracked and records nothing.\n\n--pr adopts an open pull request on macports/macports-ports instead, by URL or number: its head is fetched into a local branch, named as the pull request names it when the head is on your fork and pr/<number> when it is someone else's, and the pull request is attached, so amend updates it and sync follows it. A head of several commits is adopted as it stands; amend <port> --squash then folds it into one commit under the pull request's title, verifies it, and updates the pull request, which is how a contributor's stack of commits becomes the one the port wants without any Git by hand. A pull request from someone else's fork is pushed to like your own; GitHub allows that when the pull request permits edits by maintainers and you have write access to macports-ports, and refuses the push otherwise. --keep-body leaves the pull request's description entirely its author's; otherwise a body without a Tested on section gains one when dockhand next updates the pull request. Adopting someone's pull request evaluates their Portfile on this machine when binding the target; that judgement is yours.",
		Example: "  dockhand adopt my-branch\n  dockhand adopt my-branch newport --dry-run\n  dockhand adopt my-branch && dockhand verify newport\n  dockhand adopt --pr 34812 && dockhand amend jump --squash\n  dockhand adopt --pr https://github.com/macports/macports-ports/pull/34812 --keep-body",
		Args:    cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			request := app.AdoptRequest{DryRun: dryRun, Squash: squash, KeepBody: keepBody}
			positional := args
			if pullRequest != "" {
				ref, err := app.ParsePullRequest(pullRequest)
				if err != nil {
					return err
				}
				request.PullRequest = &ref
				if len(args) > 1 {
					return fmt.Errorf("with --pr, name at most the port")
				}
			} else {
				if len(args) == 0 {
					return fmt.Errorf("adopt needs a branch, or --pr <url|number>")
				}
				if !git.ValidBranchName(args[0]) {
					return fmt.Errorf("adopt needs a literal local branch, not %q", args[0])
				}
				request.Branch = args[0]
				positional = args[1:]
			}
			if keepBody && pullRequest == "" {
				return fmt.Errorf("--keep-body applies to a pull request; give one with --pr")
			}
			if len(positional) == 1 {
				port, err := portName(positional[0])
				if err != nil {
					return err
				}
				request.Target = port
			}
			services, err := r.build(cmd.Context(), r.config)
			if err != nil {
				return err
			}
			defer services.Close()
			progress.VerboseReport(cmd.Context(), "Fetching MacPorts master to check the branch's base")
			result, err := services.Adopt(cmd.Context(), request)
			if err != nil {
				return err
			}
			if r.json {
				return r.emit(result)
			}
			change := result.Change
			fmt.Fprintf(cmd.OutOrStdout(), "%s (%s): %s\n  Portfile: %s\n  commit %s on base %s\n", plain(change.InitiatingTarget), plain(change.Branch), map[bool]string{true: "would be tracked", false: "tracked"}[dryRun], plain(result.Portfile), result.Revision.Source.Commit, result.Revision.Source.Base)
			if result.Commits > 1 {
				fmt.Fprintf(cmd.OutOrStdout(), "  commits above master: %d\n", result.Commits)
			}
			if pr := result.PullRequest; pr != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "  pull request: %s (%s:%s)\n", plain(pr.Ref.URL), plain(pr.HeadRepository), plain(pr.HeadBranch))
			}
			if r.level(cmd) >= progress.Verbose {
				fmt.Fprintf(cmd.OutOrStdout(), "  contribution: %s; revision: %s\n", change.ID, change.CurrentRevision)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", plain(result.Detail))
			return err
		},
	}
	command.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be tracked and record nothing")
	command.Flags().BoolVar(&squash, "squash", false, "Fold a branch of several commits into one on its master base, with the oldest commit's message; the originals stay under refs/dockhand/adopted/<branch>")
	command.Flags().StringVar(&pullRequest, "pr", "", "Adopt an open pull request on macports/macports-ports, by URL or number, fetching its head into a local branch")
	command.Flags().BoolVar(&keepBody, "keep-body", false, "Leave the adopted pull request's description entirely its author's; dockhand never rewrites or appends to it")
	return command
}

// adoptHint is the way out when dockhand cannot make an edit: the person
// makes it, and dockhand still verifies and publishes it.
func adoptHint(err error) error {
	return fmt.Errorf("%w\nEdit the Portfile by hand on a branch and run dockhand adopt <branch>; dockhand verifies and publishes it from there. If dockhand should handle this Portfile itself, report the message above at %s/issues", err, version.ProjectURL)
}
