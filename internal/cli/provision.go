package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/app"
	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/platform"
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
	validate bool
	restore  bool
	purge    bool
}

func (a provisionTartAction) Execute(ctx context.Context, s *Services) error {
	t := provision.Tart{MacPorts: a.macports, CPUs: a.cpus, MemoryMB: a.memoryMB, XcodeDir: a.xcode, Tools: s.Tools}
	if a.all {
		return a.provisionAll(ctx, s, t)
	}

	if a.purge {
		// THE COUNTERPART OF PROVISIONING, and the reason `dockhand purge`
		// does not do this: an image is the provider's installation, built
		// once per release and shared by every checkout on the host, where
		// a purge clears one checkout's own work. The verb that made them
		// takes them away.
		gone, err := t.Purge(ctx, a.release)
		for _, name := range gone {
			fmt.Fprintf(s.Err, "removed %s\n", name)
		}
		if err != nil {
			return err
		}
		if len(gone) == 0 {
			fmt.Fprintf(s.Err, "no tart images for %s to remove\n", a.release.Name)
			return nil
		}
		fmt.Fprintf(s.Err, "%s has no verification images now; `dockhand provision tart --macos %s` builds one again\n",
			a.release.Name, strings.ToLower(a.release.Name))
		return nil
	}
	if a.restore {
		// The golden is the remedy D19 promises: a drifted base is
		// re-cloned from the copy nothing ever ran, which under
		// copy-on-write costs neither time nor disk.
		if err := t.Restore(ctx, a.release); err != nil {
			return err
		}
		fmt.Fprintf(s.Err, "restored %s from %s\n",
			tart.BaseName(a.release), tart.GoldenName(a.release))
		return nil
	}
	if a.validate {
		// Prove an existing base rather than rebuild it: the checks are
		// the cheap half of provisioning, and a base someone poked at
		// deserves them without the download.
		name := tart.BaseName(a.release)
		ok, err := tart.HasVM(ctx, s.Tools, name)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: no base %s to recheck; provision it first",
				verify.ErrNoEnvironment, name)
		}
		fmt.Fprintf(s.Err, "rechecking %s\n", name)
		//nolint:errcheck // the guest is detached by design
		go tart.CLI(ctx, s.Tools, nil, "run", "--no-graphics", name)
		defer func() { _, _ = tart.CLI(ctx, s.Tools, nil, "stop", name) }()
		if err := tart.WaitAgent(ctx, s.Tools, name); err != nil {
			return err
		}
		if err := t.AssertPristineFor(ctx, name); err != nil {
			return err
		}
		if v := provision.XcodeVersionOf(ctx, s.Tools, name); v != "" {
			fmt.Fprintf(s.Err, "full Xcode: %s\n", v)
		} else {
			fmt.Fprintln(s.Err, "full Xcode: none — use_xcode ports will be refused; `provision tart --xcode` adds one")
		}
		fmt.Fprintf(s.Err, "%s is what it claims: pristine, toolchain present, MacPorts answering\n", name)
		return nil
	}
	// THE NARRATION GOES TO STDERR AND THE RESULT TO STDOUT, which is
	// bump's split and now this verb's: a caller scraping stdout used to
	// get nothing at all from provision.
	line, err := t.Provision(ctx, a.release, s.Err)
	if err != nil {
		return err
	}
	fmt.Fprintln(s.Out, line)
	return nil
}

// provisionCmd builds the provision command tree: one subcommand per
// provider kind, because providers take provider-specific parameters —
// what they share is the platform vocabulary, so --macos means the same
// thing to every provider that takes it.
//
// `provision tart` is BOTH AN ACTION AND A PARENT: `provision tart
// xcode` nests under it, because baking a toolchain into a golden image
// is a TART act and the path should say which provider it belongs to
// before a second one exists. That nesting moves no KNOWLEDGE — which
// toolchain a Darwin release expects lives in internal/darwin/xcode,
// which knows nothing about virtual machines, precisely so a second
// provider could reach it. The verb is the provider's; the answer it
// acts on is not.
func provisionCmd(s *Services) *cobra.Command {
	c := &cobra.Command{
		Use:   "provision",
		Short: "Prepare verification environments",
		Args:  provisionArgs,
		// A GROUPING VERB STILL HAS TO REFUSE A STRAY WORD, and cobra will
		// not do it for a command with no Run: `execute` returns flag.ErrHelp
		// for an unrunnable command BEFORE it validates arguments, so
		// `dockhand provision xcode` printed provision's help to stdout and
		// exited 0 while provisioning nothing — a Makefile carrying the
		// pre-overhaul spelling reported success forever. Declaring the body
		// makes the command runnable, which is what puts Args in the path at
		// all; with no arguments it is what a bare `dockhand provision`
		// always did, and with one it never runs, because provisionArgs
		// refused first.
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	tart := provisionTart(s)
	tart.AddCommand(provisionXcode(s))
	c.AddCommand(tart)
	return c
}

// provisionArgs refuses a stray word under `provision` with the ruled
// usage code — an unknown subcommand is the invocation's problem, exit
// 2, the same answer `dockhand status extra` gives — and names the
// nesting for the one spelling that MOVED.
//
// `provision xcode` was the shipped verb and is now `provision tart
// xcode`, because baking a toolchain into a golden image is a tart act
// and the path should say which provider it belongs to before a second
// one exists. cobra's own suggestion machinery cannot find it: it
// searches this command's children, and the new spelling is a
// grandchild. So the one word whose meaning was relocated is answered
// by name, and every other stray word gets cobra's "unknown command".
func provisionArgs(c *cobra.Command, args []string) error {
	if len(args) > 0 && args[0] == "xcode" {
		return usagef("`dockhand provision xcode` is now `dockhand provision tart xcode`: baking a toolchain into a golden image is a tart act, and the path says so")
	}
	return noArgs(c, args)
}

func provisionTart(s *Services) *cobra.Command {
	var (
		macos    string
		macports string
		xcode    string
		cpus     int
		memoryMB int
		validate bool
		restore  bool
		purge    bool
	)
	c := &cobra.Command{
		Use:   "tart",
		Short: "Build a base VM image: vanilla macOS + guest agent + MacPorts, nothing else",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if macos == "" {
				return usagef("which macOS? pass --macos <release> (a name, a version, or \"all\")")
			}
			var release platform.Release
			all := macos == "all"
			if !all {
				r, err := parseRelease(macos)
				if err != nil {
					return err
				}
				release = r
			}
			if all && (validate || restore || purge) {
				// `all` short-circuits into provisionAll before any of these
				// branches, so an unrefused `--macos all --purge` would BUILD
				// every release rather than remove one — the opposite of what
				// was typed, at the cost of a download per release.
				return usagef("--macos all provisions; --validate, --restore and --purge take one release")
			}
			if purge && (validate || restore) {
				return usagef("--purge removes this release's images; --validate and --restore act on one that stands")
			}
			// Provisioning BUILDS the environments verification is cloned
			// from, and needs nothing else: no repository, no ports tree, no
			// evaluator, no forge. It is one of the verbs that run before
			// there is anything to maintain.
			if err := s.Acquire(cmd.Context(), app.Needs{}); err != nil {
				return err
			}
			return provisionTartAction{
				all:      all,
				release:  release,
				macports: macports,
				xcode:    xcode,
				cpus:     cpus,
				memoryMB: memoryMB,
				validate: validate,
				restore:  restore,
				purge:    purge,
			}.Execute(cmd.Context(), s)
		},
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
	// --validate, RENAMED from --recheck. The old spelling collided with
	// the intent verbs' flag of the same name, and the provisioner moved
	// because it is the rarer verb — touched once per macOS release,
	// against a spelling a maintainer reached for daily. That other
	// --recheck has since been deleted outright for duplicating
	// `refresh-checksums`, so the collision is gone and the rename now
	// stands on its own merit: --validate says what the flag does, which
	// --recheck never did.
	c.Flags().BoolVar(&validate, "validate", false,
		"validate an existing base instead of building one: re-run the pristine checks against the base already there")
	c.Flags().BoolVar(&restore, "restore", false, "replace the base with a fresh clone of its golden copy")
	// --purge is the counterpart of provisioning and NOT part of
	// `dockhand purge`, which clears a checkout's own work. An image is
	// the provider's installation — one per release, shared by every
	// checkout on the host, and expensive to rebuild — so the verb that
	// built it is the verb that removes it, and a person has to say
	// which release they mean.
	c.Flags().BoolVar(&purge, "purge", false,
		"remove this release's tart images — the vanilla base and its golden — and reclaim their disk")
	return c
}

// provisionAll sweeps every release with a base (or the modern set on
// a fresh machine), sequentially — each boot admits against the
// machine lock on its own. Sweep semantics: a release whose Xcode
// requirement cannot be met from the given archives is SKIPPED with
// the reason, not a reason to abort the ones that can proceed; hard
// provisioning failures are collected and returned together. The one
// obstacle that ends the sweep is a full machine, because it is not a
// fact about any release: the next boot would meet it too.
func (a provisionTartAction) provisionAll(ctx context.Context, s *Services, t provision.Tart) error {
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
				fmt.Fprintf(s.Err, "skipping %s: %v\n", r.Name, perr)
				continue
			}
		}
		fmt.Fprintf(s.Err, "== provisioning %s\n", r.Name)
		line, perr := t.Provision(ctx, r, s.Err)
		if perr == nil {
			fmt.Fprintln(s.Out, line)
		}
		if perr != nil {
			// A full machine ends the sweep instead of joining the tally.
			// The cap is machine-wide, so every release left would meet
			// the same refusal, and reporting that as "provisioning failed
			// for Sequoia, Sonoma, Ventura" would name three releases
			// nothing is wrong with. The single-release form answers the
			// same code for the same fact.
			if errors.Is(perr, verify.ErrNoVacancy) {
				return perr
			}
			fmt.Fprintf(s.Err, "%s failed: %v\n", r.Name, perr)
			failed = append(failed, r.Name)
		}
	}
	if len(failed) > 0 {
		return &provisionFailedError{Releases: failed}
	}
	return nil
}

// provisionFailedError is a provisioning sweep that ran and did not
// finish for every release it was given. Typed so it carries which
// ones, and so it exits in the machine band with the rest of the
// obstacles provisioning exists to clear: the sweep is the one verb
// whose whole job is fixing that band, and reporting its own failure
// as a plain failure told a script nothing about what to retry.
type provisionFailedError struct{ Releases []string }

func (e *provisionFailedError) Error() string {
	return fmt.Sprintf("provisioning failed for %s", strings.Join(e.Releases, ", "))
}

// DockhandExit: the machine band — provisioning ran and did not finish.
func (e *provisionFailedError) DockhandExit() int { return exitcode.ProvisionFailed }

// Code names the failure for a machine.
func (e *provisionFailedError) Code() string { return "provision-failed" }
