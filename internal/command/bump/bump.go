package bump

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/command/author"
	"github.com/herbygillot/dockhand/internal/command/branchflags"
	"github.com/herbygillot/dockhand/internal/command/settings"
	"github.com/herbygillot/dockhand/internal/command/ship"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term"
)

// bumpCommand is ship's single-port front, ship <port> --yes with a
// version to pin: nobody looks before it submits, so it holds what serve's
// submissions hold (the person's word that bump stays, 2026-10-15).
func Command(s *settings.Settings, streams term.Streams) *cobra.Command {
	var version author.VersionUpdate
	var plan bool
	cmd := &cobra.Command{
		Use:   "bump <port> [version]",
		Short: "Update a port, check it, and open a pull request without prompts",
		Long: `Updates a port to the newest upstream release, or the version named,
then checks and submits it without prompts. Equivalent to dockhand ship
<port> --yes, with the same unattended holds as serve. This opens a pull
request; use dockhand checksums for the checksum refresh that MacPorts'
port bump performs.

Starts a branch from freshly fetched master, or resumes an open branch
already changing the port. Updates it to the requested release, prepares
the commit, checks it, and submits exactly that commit once the check
passes. dockhand ship asks once on a terminal; dockhand update edits the
files alone. --json uses the same result format as ship.

It stops wherever a person should look:
  - The check has nowhere to build, or the port is already at that release:
    it says so, and changes nothing.
  - A newer tag was made before the port's own, and nothing newer is beyond
    it, so which release is the newest can't be told: it changes nothing,
    exits 3, and names the bump that takes that tag, if it's a release.
  - Another pull request is open for the port, or couldn't be looked for:
    it stops before downloading or building anything, and exits 3. dockhand
    ship <port> goes ahead after a look.
  - The check fails: the branch stays, with its logs.
  - The check passes, but comparing the upstream archives found what a build
    can't catch, or couldn't compare them, a commit rule has a finding, or
    another pull request was opened for the port meanwhile, as it looks
    again before submitting: the branch waits for your look, and it exits
    3. dockhand submit --branch <name> submits it after one.

The pull request's tested checkboxes stay unticked unless --tested-binaries
or --tested-variants says otherwise: they say what you tested, which
dockhand can't. The check builds where build.unattended's profile says;
--build or --on names another. --plan shows what it would do, as ship
--plan shows it, and changes nothing.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if err := version.StageReady(); err != nil {
				return err
			}
			whole := &view.BatchJSON{}
			streams.LinkSteps(whole)
			e, err := s.Open(ctx)
			if err != nil {
				return err
			}
			targets, err := ship.ShipTargets(ctx, e, streams, args[:1], branchflags.BranchFlags{})
			if err == nil && len(args) == 2 {
				targets[0].Version = args[1]
			}
			if err == nil {
				ship.WriteShipPlan(ctx, e, streams.Prose(), targets, engine.ShipSubmit)
			}
			e.Close()
			if err != nil {
				return err
			}
			if plan {
				fmt.Fprintln(streams.Prose(), "Nothing was shipped (--plan).")
				return nil
			}
			version.Linked.NoteSet = cmd.Flags().Changed("note")
			// A statement said false withdraws the one kept, as ship's and
			// submit's do, where bump read it as the flag left out (#198).
			version.Withdrawn(cmd)
			return ship.ShipAll(ctx, s, streams, whole, targets, engine.ShipSubmit, model.AnsweredYes, version)
		},
	}
	version.Flags(cmd, s)
	version.StageFlags(cmd)
	cmd.Flags().BoolVar(&plan, "plan", false, "show what it would do and change nothing")
	settings.SupportsJSON(cmd)
	return cmd
}
