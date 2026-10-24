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
		Long: `Takes a port to the newest release upstream, or the version named, and on
to its pull request, asking nothing: dockhand ship <port> --yes, with the
guardrails serve keeps. It opens a pull request, so it isn't port bump, which
refreshes checksums; dockhand checksums does that.

It updates the port on fresh master, in a branch it starts for the edit, or
resumes the open branch already changing it, brought to that release where
it isn't there; tidies the edit into one commit; checks it; and submits
exactly that commit once the check passes. dockhand ship is the same, and
asks once on a terminal; dockhand update is the edit alone. With --json,
its result is ship's.

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
				ship.WriteShipPlan(ctx, e, streams.Out, targets, engine.ShipSubmit)
			}
			e.Close()
			if err != nil {
				return err
			}
			if plan {
				fmt.Fprintln(streams.Out, "Nothing was shipped (--plan).")
				return nil
			}
			version.Linked.NoteSet = cmd.Flags().Changed("note")
			return ship.ShipAll(ctx, s, streams, whole, targets, engine.ShipSubmit, model.AnsweredYes, version)
		},
	}
	version.Flags(cmd)
	version.StageFlags(cmd)
	cmd.Flags().BoolVar(&plan, "plan", false, "show what it would do and change nothing")
	settings.SupportsJSON(cmd)
	settings.KeepModelineFlag(cmd, s)
	return cmd
}
