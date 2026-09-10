package port

import (
	"io"
	"os"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/macports"
)

// Shadow materializes a copy of this handle's portdir with its Portfile
// replaced by the given bytes, and returns a handle on that copy: the
// surface a planner evaluates to learn, exactly, what an edit would do
// before anything real is written.
//
// The returned function removes the copy. It is handed back rather than
// left to the caller because the only thing safe to remove is what
// Shadow created: a caller reaching for Target.Portdir to clean up
// would, on any other handle, be deleting a real portdir out of the
// ports tree. Skipping the call is how a shadow is kept for inspection.
//
// Regular contents (files/, patches) are copied; symlinks — a work link
// from a local build, say — are not part of the port and are skipped.
//
// The copy keeps the port's <category>/<port> identity: the shadow of
// devel/foo lives at <tmp>/devel/foo, not at <tmp> itself. That layout
// is load-bearing TWICE. Anything staging a portdir by its shape — the
// verifier's overlay, whose indexer walks categories — reads the
// category from the path. And MacPorts derives the port's TREE from it:
// macports::getportresourcepath takes [getportdir $url] and goes two
// levels up, so <tmp> is the tree this shadow belongs to.
//
// THE SHADOW IS A TREE AND NOT A LONE PORTDIR, which is the same thing
// staging says about the overlay and for the same reason. A shadow
// without the tree's _resources is not evaluated without port groups —
// getportresourcepath falls back to the DEFAULT ports tree, silently —
// so a Portfile is measured against one tree's port groups before an
// edit and another tree's after it, and the difference is attributed to
// the edit. Where the two trees agree nothing shows, which is why this
// went unnoticed: a checkout that is the configured default, or matches
// it, cannot expose it. A tree carrying a port group the default lacks
// fails outright with "PortGroup not found"; one carrying a different
// version of the same group quietly answers a different question.
//
// IT IS COPIED, ON THE SAME TERMS AS THE PORTDIR, and a link would have
// been the cheaper wrong answer. Measured on the real tree, 1.4 MB
// across 145 files costs 18ms against 0.17ms to link — but a linked
// shadow is not a copy of anything, it is a window onto the tree it was
// made from. Two consequences follow, and both are the reason a shadow
// exists at all. A change that must EDIT a resource — rust's stage0
// checksums live in rust_build-1.0.tcl, and the rust Portfile's own
// header says a version bump edits them — could never be predicted,
// because the shadow would evaluate the tree's unedited copy and report
// that the edit changed nothing. And anything that wrote there would be
// writing into the user's checkout, from a road whose whole promise is
// that it changes nothing real.
func (h Handle) Shadow(portfile []byte) (Handle, func(), error) {
	root, remove, err := h.TempDir.MakeDir("shadow")
	if err != nil {
		return Handle{}, nil, err
	}
	clean := filepath.Clean(h.Target.Portdir)
	dir := filepath.Join(root, filepath.Base(filepath.Dir(clean)), filepath.Base(clean))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		remove()
		return Handle{}, nil, err
	}
	if err := copyTree(dir, h.Target.Portdir); err != nil {
		remove()
		return Handle{}, nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, macports.PortfileName), portfile, 0o644); err != nil {
		remove()
		return Handle{}, nil, err
	}
	if err := copyResources(root, clean); err != nil {
		remove()
		return Handle{}, nil, err
	}
	return h.At(dir), remove, nil
}

// copyResources gives the shadow tree its own _resources, so port
// groups, mirror lists and the rest resolve to what the port actually
// has rather than to the installation's default tree — and so that
// anything written there is written to the shadow.
//
// The source tree is found by MacPorts' own rule: two levels up from
// the portdir, which is what getportresourcepath does. The shadow and
// the evaluator agree about which tree a portdir belongs to without
// either being told.
//
// The whole directory is taken rather than the port groups alone. That
// is the tree's ruling — _resources is named and not pattern-matched —
// and it is what keeps this honest about a Portfile reaching for a
// mirror list or a compiler resource rather than a group.
//
// A tree with no _resources is left alone rather than made to have one.
// That is not a failure: it is what a bare portdir in a temporary
// directory is, every fixture in this suite is one, and the fallback
// that serves them is the behaviour they already rely on.
func copyResources(shadowRoot, portdir string) error {
	src := filepath.Join(filepath.Dir(filepath.Dir(portdir)), macports.ResourcesDir)
	info, err := os.Stat(src)
	if err != nil || !info.IsDir() {
		return nil //nolint:nilerr // no resources to carry is not a failure
	}
	return copyTree(filepath.Join(shadowRoot, macports.ResourcesDir), src)
}

// copyTree copies a portdir's regular contents into dst.
func copyTree(dst, srcDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.Type()&os.ModeSymlink != 0:
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			return copyFile(target, path)
		default:
			return nil
		}
	})
}

func copyFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck // read-path close
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close() //nolint:errcheck // best-effort on the error path
		return err
	}
	return out.Close()
}
