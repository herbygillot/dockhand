package bump

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/platform"
)

// staleElsewhere answers whether this edit would leave a checksums
// command in a branch this host did not take describing the release
// before — and it answers by MEASURING, not by reading a filename.
//
// A bump rewrites the checksums it could fetch. A command in an untaken
// branch is left as written, and whether that is a defect depends on
// what that branch fetches once the edit lands:
//
//   - gh's binary branch names gh_${version}_macOS_amd64.zip. Apply the
//     version edit and that branch's distfile changes, so its digests
//     now describe a file the port no longer fetches. Stale.
//   - cliclick pins github.setup BlueM cliclick 4.0.1 in its legacy
//     branch, and LyX pins 2.3.8 in two of its three. Apply the edit and
//     those branches fetch exactly what they fetched before. Untouched,
//     correctly.
//
// SO THE EDIT IS SHADOWED IN THE OTHER FRAME AND THE FETCH COMPARED.
// This replaces asking whether the other branch's distfile name contains
// the version being moved, which is what this did first and which is
// inference in the one place where being wrong writes bad bytes: a
// distfile whose name does not carry the version while its content
// tracks it would have been cleared wrongly. The question was never
// about the name. It is whether the edit MOVES the fetch, and that is
// answerable directly.
//
// It is the same shadow every other prediction in this tool is made
// against, evaluated somewhere this host is not — which is also the
// guard a re-derivation of those digests will need, since the ordinary
// shadow prediction runs in THIS frame and cannot see an edit written
// into a branch it does not take.
//
// WITHOUT A FRAME CAPABILITY IT IS STALE, which keeps the refusal that
// shipped. A road with no evaluator pool to spend cannot tell a pinned
// branch from a moving one, and the honest answer where a defect cannot
// be ruled out is to decline. This only ever removes a refusal it has
// earned the right to remove.
func staleElsewhere(ctx context.Context, frames framer, here info.Values, src []byte, edits []edit.Edit) (string, bool) {
	if frames == nil {
		return "this machine", true
	}
	edited, err := edit.Apply(src, edits)
	if err != nil {
		return "this machine", true
	}
	measured := strings.Join(here.Distfiles, " ")
	looked := false
	for _, r := range platform.Releases {
		for _, arch := range []string{"arm", "i386"} {
			f := info.Platform{OS: "macosx", Major: r.Darwin, Arch: arch}
			before, err := frames(ctx, f, nil)
			if err != nil {
				continue
			}
			was := strings.Join(before.Distfiles, " ")
			if was == "" || was == measured {
				continue // this frame takes the branch already accounted for
			}
			after, err := frames(ctx, f, edited)
			if err != nil {
				continue
			}
			looked = true
			if strings.Join(after.Distfiles, " ") != was {
				return fmt.Sprintf("%s/%s", r.Name, arch), true
			}
		}
	}
	// Every other branch fetches after the edit exactly what it fetched
	// before it. Nothing there goes stale.
	//
	// A port whose frames all took the branch measured here taught this
	// nothing, and falls back to the refusal rather than to silence.
	return "", !looked
}

// framer is the frame capability as this package takes it: evaluate this
// port under another platform, optionally over bytes an edit would
// write rather than the Portfile as it stands.
type framer = func(ctx context.Context, p info.Platform, src []byte) (info.Values, error)
