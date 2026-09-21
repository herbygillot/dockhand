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
	var dryRun, squash bool
	command := &cobra.Command{
		Use: "adopt <branch> [port]", Short: "Track a branch you prepared by hand as a contribution",
		Long:    "Adopt a branch dockhand did not make: one commit above a commit of MacPorts master, changing one port directory, whose port is inferred from that directory unless named. Once tracked, every verb selects it by port name: verify builds it, publish opens its pull request, amend and rebase revise it, and status shows it. This is the way in for a Portfile dockhand cannot edit itself, and for a new port. The branch stays where it is under its own name. A branch with several commits is refused unless --squash folds them into one, keeping the originals under a backup ref; changes in several port directories are refused and named. --dry-run reports what would be tracked and records nothing.",
		Example: "  dockhand adopt my-branch\n  dockhand adopt my-branch newport --dry-run\n  dockhand adopt my-branch && dockhand verify newport",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !git.ValidBranchName(args[0]) {
				return fmt.Errorf("adopt needs a literal local branch, not %q", args[0])
			}
			request := app.AdoptRequest{Branch: args[0], DryRun: dryRun, Squash: squash}
			if len(args) == 2 {
				port, err := portName(args[1])
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
			if r.level(cmd) >= progress.Verbose {
				fmt.Fprintf(cmd.OutOrStdout(), "  contribution: %s; revision: %s\n", change.ID, change.CurrentRevision)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", plain(result.Detail))
			return err
		},
	}
	command.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be tracked and record nothing")
	command.Flags().BoolVar(&squash, "squash", false, "Fold a branch of several commits into one on its master base, with the oldest commit's message; the originals stay under refs/dockhand/adopted/<branch>")
	return command
}

// adoptHint is the way out when dockhand cannot make an edit: the person
// makes it, and dockhand still verifies and publishes it.
func adoptHint(err error) error {
	return fmt.Errorf("%w\nEdit the Portfile by hand on a branch and run dockhand adopt <branch>; dockhand verifies and publishes it from there. If dockhand should handle this Portfile itself, report the message above at %s/issues", err, version.ProjectURL)
}
