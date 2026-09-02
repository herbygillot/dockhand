package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/verify/tart"
	"github.com/herbygillot/dockhand/internal/verify/tart/provision"
)

// provisionTartAction builds, rechecks, or restores a tart base image.
type provisionTartAction struct {
	all      bool
	release  platform.Release
	macports string
	xcode    string
	cpus     int
	memoryMB int
	recheck  bool
	restore  bool
}

var _ Action = provisionTartAction{}

func (a provisionTartAction) Execute(ctx context.Context, rs *runstate.Context) error {
	t := provision.Tart{MacPorts: a.macports, CPUs: a.cpus, MemoryMB: a.memoryMB, XcodeDir: a.xcode, Tools: rs.Tools}
	if a.all {
		return a.provisionAll(ctx, rs, t)
	}

	if a.restore {
		// The golden is the remedy D19 promises: a drifted base is
		// re-cloned from the copy nothing ever ran, which under
		// copy-on-write costs neither time nor disk.
		if err := t.Restore(ctx, a.release); err != nil {
			return err
		}
		fmt.Fprintf(rs.Err, "restored %s from %s\n",
			tart.BaseName(a.release), tart.GoldenName(a.release))
		return nil
	}
	if a.recheck {
		// Prove an existing base rather than rebuild it: the checks are
		// the cheap half of provisioning, and a base someone poked at
		// deserves them without the download.
		name := tart.BaseName(a.release)
		ok, err := tart.HasVM(ctx, rs.Tools, name)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: no base %s to recheck; provision it first",
				verify.ErrNoEnvironment, name)
		}
		fmt.Fprintf(rs.Err, "rechecking %s\n", name)
		//nolint:errcheck // the guest is detached by design
		go tart.CLI(ctx, rs.Tools, nil, "run", "--no-graphics", name)
		defer func() { _, _ = tart.CLI(ctx, rs.Tools, nil, "stop", name) }()
		if err := tart.WaitAgent(ctx, rs.Tools, name); err != nil {
			return err
		}
		if err := t.AssertPristineFor(ctx, name); err != nil {
			return err
		}
		if v := provision.XcodeVersionOf(ctx, rs.Tools, name); v != "" {
			fmt.Fprintf(rs.Err, "full Xcode: %s\n", v)
		} else {
			fmt.Fprintln(rs.Err, "full Xcode: none — use_xcode ports will be refused; `provision tart --xcode` adds one")
		}
		fmt.Fprintf(rs.Err, "%s is what it claims: pristine, toolchain present, MacPorts answering\n", name)
		return nil
	}
	return t.Provision(ctx, a.release, rs.Err)
}

// Provision builds the provision command tree: one subcommand per
// provider kind, because providers take provider-specific parameters —
// what they share is the platform vocabulary, so --macos means the
// same thing to every provider that takes it.
func Provision() *cobra.Command {
	c := &cobra.Command{
		Use:   "provision",
		Short: "Prepare verification environments",
	}
	c.AddCommand(provisionTart(), provisionXcode())
	return c
}

func provisionTart() *cobra.Command {
	var (
		macos    string
		macports string
		xcode    string
		cpus     int
		memoryMB int
		recheck  bool
		restore  bool
	)
	c := &cobra.Command{
		Use:   "tart",
		Short: "Build a base VM image: vanilla macOS + guest agent + MacPorts, nothing else",
		Args:  noArgs,
		RunE: runE(func(*cobra.Command, []string) (Action, error) {
			if macos == "" {
				return nil, usagef("which macOS? pass --macos <release> (a name, a version, or \"all\")")
			}
			var release platform.Release
			all := macos == "all"
			if !all {
				r, err := parseRelease(macos)
				if err != nil {
					return nil, err
				}
				release = r
			}
			if all && (recheck || restore) {
				return nil, usagef("--macos all provisions; recheck and restore take one release")
			}
			return provisionTartAction{
				all:      all,
				release:  release,
				macports: macports,
				xcode:    xcode,
				cpus:     cpus,
				memoryMB: memoryMB,
				recheck:  recheck,
				restore:  restore,
			}, nil
		}),
	}
	c.Flags().StringVar(&macos, "macos", "", "macOS release to provision (name or version)")
	c.Flags().StringVar(&macports, "macports", "",
		"MacPorts version to install (default: the newest dockhand has a shim for)")
	c.Flags().StringVar(&xcode, "xcode", "",
		"directory of Xcode .xip archives (or one .xip); installs the newest the release can run")
	c.Flags().IntVar(&cpus, "cpus", 0,
		"CPU cores per VM (default: half the host's physical cores)")
	c.Flags().IntVar(&memoryMB, "memory", 0,
		"memory per VM in MB (default: 2048 per core)")
	c.Flags().BoolVar(&recheck, "recheck", false, "re-run the pristine checks on an existing base instead of building one")
	c.Flags().BoolVar(&restore, "restore", false, "replace the base with a fresh clone of its golden copy")
	return c
}

// provisionAll sweeps every release with a base (or the modern set on
// a fresh machine), sequentially — each boot admits against the
// machine lock on its own. Sweep semantics: a release whose Xcode
// requirement cannot be met from the given archives is SKIPPED with
// the reason, not a reason to abort the ones that can proceed; hard
// provisioning failures are collected and returned together.
func (a provisionTartAction) provisionAll(ctx context.Context, rs *runstate.Context, t provision.Tart) error {
	releases, err := t.Provisioned(ctx)
	if err != nil {
		return err
	}
	if len(releases) == 0 {
		releases = modernReleases()
	}
	var failed []string
	for _, r := range releases {
		if a.xcode != "" {
			if _, _, perr := provision.PickXcode(a.xcode, r); perr != nil {
				fmt.Fprintf(rs.Err, "skipping %s: %v\n", r.Name, perr)
				continue
			}
		}
		fmt.Fprintf(rs.Err, "== provisioning %s\n", r.Name)
		if perr := t.Provision(ctx, r, rs.Err); perr != nil {
			fmt.Fprintf(rs.Err, "%s failed: %v\n", r.Name, perr)
			failed = append(failed, r.Name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("provisioning failed for %s", strings.Join(failed, ", "))
	}
	return nil
}
