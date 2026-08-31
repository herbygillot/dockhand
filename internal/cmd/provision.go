package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/runcontext"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/herbygillot/dockhand/internal/verify/tart/provision"
)

// Provision builds the provision command tree: one subcommand per
// provider kind, because providers take provider-specific parameters —
// what they share is the platform vocabulary, so --macos means the
// same thing to every provider that takes it.
func Provision(rc *runcontext.RunContext) *cobra.Command {
	c := &cobra.Command{
		Use:   "provision",
		Short: "Prepare verification environments",
	}
	c.AddCommand(provisionTart(rc))
	return c
}

func provisionTart(rc *runcontext.RunContext) *cobra.Command {
	var (
		macos    string
		macports string
		recheck  bool
		restore  bool
	)
	c := &cobra.Command{
		Use:   "tart",
		Short: "Build a base VM image: vanilla macOS + guest agent + MacPorts, nothing else",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if macos == "" {
				return usagef("which macOS? pass --macos <release> (a name like sequoia, or a version like 15)")
			}
			release, err := platform.Parse(macos)
			if err != nil {
				return &UsageError{Err: err}
			}
			t := provision.Tart{MacPorts: macports}

			if restore {
				// The golden is the remedy D19 promises: a drifted base
				// is re-cloned from the copy nothing ever ran, which
				// under copy-on-write costs neither time nor disk.
				if err := t.Restore(cmd.Context(), release); err != nil {
					return err
				}
				fmt.Fprintf(rc.Err, "restored %s from %s\n",
					tart.BaseName(release), tart.GoldenName(release))
				return nil
			}
			if recheck {
				// Prove an existing base rather than rebuild it: the
				// checks are the cheap half of provisioning, and a base
				// someone poked at deserves them without the download.
				name := tart.BaseName(release)
				ok, err := tart.HasVM(cmd.Context(), name)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("%w: no base %s to recheck; provision it first",
						verify.ErrNoEnvironment, name)
				}
				fmt.Fprintf(rc.Err, "rechecking %s\n", name)
				//nolint:errcheck // the guest is detached by design
				go tart.CLI(cmd.Context(), nil, "run", "--no-graphics", name)
				defer func() { _, _ = tart.CLI(cmd.Context(), nil, "stop", name) }()
				if err := tart.WaitAgent(cmd.Context(), name); err != nil {
					return err
				}
				if err := t.AssertPristineFor(cmd.Context(), name); err != nil {
					return err
				}
				fmt.Fprintf(rc.Err, "%s is what it claims: pristine, toolchain present, MacPorts answering\n", name)
				return nil
			}
			return t.Provision(cmd.Context(), release, rc.Err)
		},
	}
	c.Flags().StringVar(&macos, "macos", "", "macOS release to provision (name or version)")
	c.Flags().StringVar(&macports, "macports", "",
		"MacPorts version to install (default: the newest dockhand has a shim for)")
	c.Flags().BoolVar(&recheck, "recheck", false, "re-run the pristine checks on an existing base instead of building one")
	c.Flags().BoolVar(&restore, "restore", false, "replace the base with a fresh clone of its golden copy")
	return c
}
