package command

import "github.com/spf13/cobra"

// bumpCommand is update --new --submit --yes, asking nothing: nobody looks
// before it submits, so it holds what serve's submissions hold.
func bumpCommand(s *settings, streams Streams) *cobra.Command {
	var version versionUpdate
	cmd := &cobra.Command{
		Use:   "bump <port> [version]",
		Short: "Update a port and open its pull request, asking nothing",
		Long: `Takes a port to the newest release upstream, or the version named, and on
to its pull request, asking nothing: update --new --submit --yes, with the
guardrails serve keeps. It opens a pull request, so it isn't port bump, which
refreshes checksums; dockhand checksums does that.

It updates the port on fresh master, in a branch it starts for the edit,
tidies the edit into one commit, checks it, and submits exactly that commit
once the check passes. dockhand update is the same work a step at a time,
looking and editing along the way. With --json, its result is update
--submit's: the update's, with tidy's, check's, and submit's inside it.

It stops wherever a person should look:
  - A branch already changes the port, the check has nowhere to build, or
    the port is already at that release: it says so, and changes nothing.
  - The check fails: the branch stays, with its logs.
  - The check passes, but comparing the upstream archives found what a build
    can't catch, a commit rule has a finding, or another pull request is open
    for the port: the branch waits for your look, and it exits 3. dockhand
    submit --branch <name> submits it after one.

The pull request's tested checkboxes stay unticked unless --tested-binaries
or --tested-variants says otherwise: they say what you tested, which
dockhand can't. --on says where to check (default check.on).`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			request, err := version.request(args, false)
			if err != nil {
				return err
			}
			version.linked.submit, version.linked.yes, version.linked.unattended = true, true, true
			return version.run(cmd.Context(), s, streams.unattended(), branchChoice{new: true}, request)
		},
	}
	version.flags(cmd, "")
	return cmd
}
