package bump

import (
	"context"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/platform"
)

// tracksVersion answers whether a checksums command this evaluation
// never reached would GO STALE if the version moved.
//
// A bump rewrites the checksums it could measure. A command in a branch
// this host did not take is left as written — and whether that is a
// defect depends entirely on what that branch fetches:
//
//   - gh's binary branch names `gh_${version}_macOS_amd64.zip`, so a
//     bump renames the file under digests it did not re-derive. Stale.
//   - cliclick's legacy branch pins `github.setup BlueM cliclick 4.0.1`
//     beside its own 32-bit and c89 patches, deliberately held back for
//     systems the 5.x line dropped. A bump of 5.0.1 must not touch it.
//
// The two are indistinguishable in the taken evaluation, which sees
// neither. So the other branches are EVALUATED — each frame the release
// table can name, on both architectures — and the question asked of what
// they actually fetch: does any set of distfiles this port produces,
// other than the one measured here, carry the version being moved?
//
// It is observation and not inference. Reading the source for a
// `${version}` reference would be guessing at a Tcl expansion; asking
// the evaluator what the filename came out as is the fact itself.
//
// WITHOUT A FRAME CAPABILITY IT ANSWERS TRUE, which keeps the refusal
// that shipped: a road with no evaluator pool to spend cannot tell a
// pinned branch from a stale one, and the honest answer where a defect
// cannot be ruled out is to decline. The refinement only ever removes a
// refusal it has earned the right to remove.
func tracksVersion(ctx context.Context, frames func(context.Context, info.Platform) (info.Values, error), here info.Values, version string) bool {
	if frames == nil || version == "" {
		return true
	}
	measured := strings.Join(here.Distfiles, " ")
	seen := false
	for _, r := range platform.Releases {
		for _, arch := range []string{"arm", "i386"} {
			vals, err := frames(ctx, info.Platform{OS: "macosx", Major: r.Darwin, Arch: arch})
			if err != nil {
				continue
			}
			other := strings.Join(vals.Distfiles, " ")
			if other == "" || other == measured {
				continue
			}
			seen = true
			if strings.Contains(other, version) {
				// Another branch fetches a file named for the version this
				// bump is moving: renaming it without re-deriving its
				// digests is exactly the defect.
				return true
			}
		}
	}
	// Every other branch fetches something whose name the version does
	// not reach — a pinned legacy release, or a file versioned on its
	// own. Nothing here goes stale.
	//
	// A port whose frames all produced the same distfiles tells us
	// nothing either way, and falls back to the refusal.
	return !seen
}
