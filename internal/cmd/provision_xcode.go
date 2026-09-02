package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/runstate"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify/tart/provision"
)

// provisionXcodeAction is the guided half of Xcode provisioning: it
// runs no VM and downloads nothing — Apple's downloads sit behind an
// Apple ID, so the human does that step — but it removes every other
// question: WHICH Xcode each macOS release can run (the version
// matters greatly, and Apple raises the floor mid-line), where to get
// it, and the exact command that bakes it into a golden image. With
// --from it also assesses a directory of archives and says per release
// whether the right one is already on hand.
type provisionXcodeAction struct {
	release platform.Release // zero means every release with a base
	from    string
}

var _ Action = provisionXcodeAction{}

func (a provisionXcodeAction) Execute(ctx context.Context, rs *runstate.Context) error {
	releases, err := a.targets(ctx, rs.Tools)
	if err != nil {
		return err
	}
	fmt.Fprintln(rs.Out, "Xcode provisioning — what each release needs:")
	fmt.Fprintln(rs.Out)
	for _, r := range releases {
		version, capped := provision.RecommendedXcode(r)
		want := "the newest Xcode release"
		archive := "Xcode_<version>.xip"
		if capped {
			want = "Xcode " + version + " (newer refuses to run there)"
			archive = "Xcode_" + version + ".xip"
		}
		fmt.Fprintf(rs.Out, "  %-10s needs %s\n", r.Name, want)
		if a.from != "" {
			switch path, v, err := provision.PickXcode(a.from, r); {
			case err != nil:
				fmt.Fprintf(rs.Out, "  %-10s missing: place %s in %s\n", "", archive, a.from)
			case capped && v != version:
				// Usable is not ready: an older archive provisions, but
				// the release could carry more.
				fmt.Fprintf(rs.Out, "  %-10s usable: %s (Xcode %s — below the recommended %s)\n", "", path, v, version)
			default:
				fmt.Fprintf(rs.Out, "  %-10s ready: %s (Xcode %s)\n", "", path, v)
			}
		}
	}
	dir := a.from
	if dir == "" {
		dir = "~/Downloads/xcode-archives"
	}
	fmt.Fprintf(rs.Out, `
The steps:
  1. Sign in at https://developer.apple.com/download/all/?q=Xcode
     (an Apple ID is required; dockhand never handles it) and download
     each version named above as its .xip archive. Apple offers each
     as "Apple Silicon" or "Universal" — either works here.
     https://xcodereleases.com indexes every version with direct links.
  2. Place the archives in one directory, named as Apple ships them
     (Xcode_16.2.xip, Xcode_26.3_Apple_silicon.xip): %s
  3. Bake each golden image:
       dockhand provision tart --macos <release> --xcode %s
     The newest archive the release can run is chosen automatically;
     expect roughly +40 GB per image and a slow xip verification.

A base provisioned this way verifies use_xcode ports instead of
refusing them; re-run with --from %s to check readiness.
`, dir, dir, dir)
	return nil
}

// targets picks which releases to explain: the named one, else every
// release with a provisioned base, else the modern set.
func (a provisionXcodeAction) targets(ctx context.Context, tools *tool.Finder) ([]platform.Release, error) {
	if !a.release.IsZero() {
		return []platform.Release{a.release}, nil
	}
	if tools.Have(tool.Tart) {
		if rels, err := (provision.Tart{Tools: tools}).Provisioned(ctx); err == nil && len(rels) > 0 {
			return rels, nil
		}
	}
	return modernReleases(), nil
}

// provisionXcode builds the `provision xcode` subcommand.
func provisionXcode() *cobra.Command {
	var macos, from string
	c := &cobra.Command{
		Use:   "xcode",
		Short: "Guide through provisioning Xcode-bearing golden images",
		Args:  noArgs,
		RunE: runE(func(*cobra.Command, []string) (Action, error) {
			var release platform.Release
			if macos != "" {
				r, err := parseRelease(macos)
				if err != nil {
					return nil, err
				}
				release = r
			}
			return provisionXcodeAction{release: release, from: from}, nil
		}),
	}
	c.Flags().StringVar(&macos, "macos", "", "explain one macOS release (default: every provisioned base)")
	c.Flags().StringVar(&from, "from", "", "directory of downloaded .xip archives to assess for readiness")
	return c
}
