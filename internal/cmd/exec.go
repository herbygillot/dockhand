package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify/tart"
)

// execAction runs one command on pristine clones of provisioned bases:
// the cheap question the verification pipeline is too heavy for. Field
// evidence made the case — bracketing which macOS releases carry a
// symbol took five hand-rolled clone/boot/probe/delete cycles at
// seconds each, against ten-minute builds to learn the same fact.
// Sequential on purpose: probes share the two-guest cap with real
// verifications, and one slot briefly borrowed beats two occupied.
type execAction struct {
	on   []string // releases to probe; empty means the newest, "all" means every base
	argv []string
}

var _ Action = execAction{}

func (a execAction) Execute(ctx context.Context, rs *runstate.Context) error {
	prov, err := rs.VerifyProvider(ctx)
	if err != nil {
		return err
	}
	releases, err := resolveReleaseSet(a.on, prov.Capabilities().Platforms, false)
	if err != nil {
		return err
	}
	var failed int
	for _, r := range releases {
		fmt.Fprintf(rs.Err, "=== %s\n", r)
		out, err := tart.RunOnBase(ctx, rs.Tools, tart.BaseName(r), a.argv)
		if out != "" {
			fmt.Fprintln(rs.Out, strings.TrimRight(out, "\n"))
		}
		if err != nil {
			failed++
			fmt.Fprintf(rs.Err, "%s: %v\n", r.Name, err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	if failed > 0 {
		return fmt.Errorf("exec: the command failed on %d of %d releases", failed, len(releases))
	}
	return nil
}

// Exec builds the exec subcommand.
func Exec() *cobra.Command {
	var on []string
	c := &cobra.Command{
		Use:   "exec [--on <release>[,<release>]|--on all] -- <command> [args...]",
		Short: "Run a command on pristine clones of provisioned bases",
		Args: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usagef("exec needs a command to run")
			}
			return nil
		},
		RunE: runE(func(_ *cobra.Command, args []string) (Action, error) {
			return execAction{on: on, argv: args}, nil
		}),
	}
	c.Flags().StringSliceVar(&on, "on", nil,
		`macOS releases to probe, or "all" (default: the newest provisioned base)`)
	return c
}
