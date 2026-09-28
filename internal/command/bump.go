package command

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

func bumpCommand(s *settings, streams Streams) *cobra.Command {
	var linked linkedOptions
	var keepOld, shared bool
	cmd := &cobra.Command{
		Use:   "bump <port> [version]",
		Short: "Update a port and open its pull request, asking nothing",
		Long: `Takes a port to the newest release upstream, or the version named, and on
to its pull request, asking nothing: update --new --submit --yes, with the
guardrails serve keeps. It opens a pull request, so it isn't port bump, which
refreshes checksums; dockhand checksums does that.

It starts a branch from fresh master, updates the port, tidies the edit into
one commit, checks it, and submits exactly that commit once the check
passes. dockhand update is the same work a step at a time, looking and
editing along the way.

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
			if len(linked.except) > 0 && !linked.revbump {
				return errors.New("--except takes a port out of --revbump-dependents; add --revbump-dependents")
			}
			request := engine.UpdateRequest{Action: model.EditUpdate, Port: args[0], KeepOldChecksums: keepOld, SharedRelease: shared, CompareUpstream: true}
			if len(args) == 2 {
				request.Version = args[1]
			}
			return bump(cmd.Context(), s, streams.unattended(), request, linked)
		},
	}
	cmd.Flags().StringArrayVar(&linked.on, "on", nil, "where to check (default check.on)")
	cmd.Flags().BoolVar(&linked.testedBinaries, "tested-binaries", false, "state that you tested the basic functionality of all binary files")
	cmd.Flags().BoolVar(&linked.testedVariants, "tested-variants", false, "state that you checked the most important variants")
	cmd.Flags().BoolVar(&linked.revbump, "revbump-dependents", false, "also bump the revision of the ports that link it directly")
	cmd.Flags().StringSliceVar(&linked.except, "except", nil, "leave this dependent out of --revbump-dependents")
	cmd.Flags().BoolVar(&shared, "shared-release", false, "move every subport that shares the port's release")
	cmd.Flags().BoolVar(&keepOld, "keep-old-checksums", false, "refresh legacy md5 or sha1 checksums in place rather than rewriting them as rmd160, sha256, and size")
	return cmd
}

// bump is update --new --submit --yes, asking nothing. What would stop it
// before the edit is settled first, so stopping there changes nothing.
func bump(ctx context.Context, s *settings, streams Streams, request engine.UpdateRequest, linked linkedOptions) error {
	if ready, err := bumpable(ctx, s, streams, request, linked.on); err != nil || !ready {
		return err
	}
	linked.submit, linked.yes, linked.unattended = true, true, true
	branch, update, err := author(ctx, s, streams, branchChoice{new: true}, "update", request, linked)
	if err != nil || !update.Applied {
		return err
	}
	return tidyAndSubmit(ctx, s, streams, branch, linked)
}

// bumpable reports whether a bump has work to do: no open branch changes
// the port already, the check has somewhere to build, and the port isn't
// at the release asked for. The newest release is found as outdated finds
// it; a port it can't look up is left for the update to try.
func bumpable(ctx context.Context, s *settings, streams Streams, request engine.UpdateRequest, on []string) (bool, error) {
	e, err := s.open(ctx)
	if err != nil {
		return false, err
	}
	defer e.Close()
	statuses, err := e.Status(ctx)
	if err != nil {
		return false, err
	}
	var names []string
	for _, status := range statuses {
		if slices.Contains(status.Scope.PortNames(), request.Port) {
			names = append(names, status.Branch.ShortName())
		}
	}
	if len(names) > 0 {
		return false, fmt.Errorf("%s is already changed in %s, so nothing was changed; dockhand status %s says what it needs", request.Port, strings.Join(names, ", "), names[0])
	}
	if _, err := e.Environments(ctx, firstNonEmpty(on, s.file.Check.On)); err != nil {
		return false, fmt.Errorf("%w; nothing was changed", err)
	}
	report, err := e.Outdated(ctx, engine.OutdatedRequest{Ports: []string{request.Port}})
	if err != nil {
		return false, fmt.Errorf("looking up %s's newest release: %w; nothing was changed", request.Port, err)
	}
	found := slices.IndexFunc(report.Ports, func(port engine.OutdatedPort) bool { return port.Port == request.Port })
	if found < 0 {
		return true, nil
	}
	port := report.Ports[found]
	switch {
	case request.Version != "":
		if request.Version == port.Current {
			fmt.Fprintf(streams.Out, "%s is already at %s; nothing to change.\n", port.Port, port.Current)
			return false, nil
		}
	case port.Problem != "":
		return false, fmt.Errorf("can't tell whether %s has a newer release: %s; nothing was changed", port.Port, port.Problem)
	case !port.Outdated:
		fmt.Fprintf(streams.Out, "%s is already at %s, the newest release; nothing to change.\n", port.Port, port.Current)
		return false, nil
	}
	return true, nil
}
