package bump

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"

	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/patch"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tool"
)

// filesDir is the directory under the portdir the patch phase reads a
// patchfile from, and the first component of every path a relocation
// puts on the plan. It is spelled here rather than read off
// vals.Filespath because a plan's file paths are portdir-relative by
// contract, and the evaluated filespath is an absolute path into
// whichever portdir — the real one or a shadow — answered the question.
const filesDir = "files"

// patchTag is the fetch tag a Portfile may hang on a patchfile's name
// (patch-foo.diff:tag) to choose a patch_sites group. It is not part
// of the file name, and base's getdistname strips it with this same
// expression before looking for the file.
var patchTag = regexp.MustCompile(`:[0-9A-Za-z_-]+$`)

// relocatePatches moves the port's patches onto the source a bump just
// fetched, by the one move the ruling allows (2026-09-04): each hunk's
// before-block is looked for verbatim in the new file, and where it
// occurs exactly once the hunk's @@ numbers are rewritten to say so.
// Nothing inside a hunk changes. A patch every hunk of which is already
// where it was produces nothing; a patch with a hunk that moved
// produces a FileEdit carrying the whole refreshed patch; and a patch
// with a hunk that does not relocate — its lines are not there, are
// there twice, sit in a file the distfile does not carry, or land on
// another hunk — declines the bump. The whole patch and the whole bump,
// not the hunk: a patch half refreshed is a patch nobody wrote, and a
// bump that ships one is the complete-looking wrong artifact this tool
// promises against.
//
// The names returned beside the edits are the patchfiles that moved,
// as the Portfile spells them, for the sentence the plan's summary says
// about them. Both are nil when the port has no patchfiles.
//
// The file is read from the portdir's files/ directory, which is where
// the patch phase looks first. A patchfile that is not there is one the
// port fetches from patch_sites, and dockhand cannot refresh a patch it
// does not carry: that is a decline too, saying so, rather than a bump
// that silently applies an unrefreshed patch to a source it may not
// fit. A compressed patch (.gz, .bz2, .xz — base decompresses them on
// the way to patch(1)) is not a unified diff and declines the same way,
// from the parser.
//
// Every decline here is ByNetwork. Two things decided it that the memo
// key does not hold: the patch's own bytes, which a maintainer rewrites
// without touching the Portfile, and the source the hunks were looked
// for in, which is whatever the server served for the new version.
func relocatePatches(ctx context.Context, tools *tool.Finder, portdir string, vals info.Values, worksrcdir string, fetched []string) ([]plan.FileEdit, []string, error) {
	strip := eval.StripLevel(vals.PatchPreArgs)
	// The reader hands Relocate each target out of the fetched
	// distfiles at exactly worksrcdir/<target>: the directory the port
	// evaluates its source into is the directory the patch phase runs
	// patch(1) in, and that file is the one patch(1) will open. Exactly
	// that member and no other — not distfile.Extract, whose fallback
	// to the shallowest copy of a bare name serves a go.mod a project
	// may keep anywhere, and would here hand back a nested Makefile in
	// place of the top-level one a release dropped, relocate the hunk
	// onto it, and mint a patch whose phase fails. A member that is not
	// there is the give-up the ruling asks for. What is read and what
	// was hashed for the checksums are the same bytes, so the refreshed
	// patch describes the source the plan records.
	read := func(target string) ([]byte, error) {
		body, from, err := distfile.ExtractMember(ctx, tools, fetched, path.Join(worksrcdir, target))
		if err == nil {
			slog.Debug("read patch target", "file", target, "from", filepath.Base(from))
		}
		return body, err
	}

	var files []plan.FileEdit
	var moved []string
	for _, pf := range vals.Patchfiles {
		name := patchTag.ReplaceAllString(pf, "")
		rel := filesDir + "/" + name
		src, err := os.ReadFile(filepath.Join(portdir, filesDir, filepath.FromSlash(name)))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil, nil, &plan.Decline{Type: plan.PatchWontRelocate,
				Detail:     rel + " is not in the portdir; a patch the port fetches from patch_sites is not dockhand's to refresh",
				Determined: plan.ByNetwork}
		case err != nil:
			return nil, nil, fmt.Errorf("bump: %w", err)
		}
		p, err := patch.Parse(src)
		if err != nil {
			return nil, nil, &plan.Decline{Type: plan.PatchWontRelocate,
				Detail:     rel + ": " + err.Error(),
				Determined: plan.ByNetwork}
		}
		res, err := p.Relocate(read, strip)
		var re *patch.RelocateError
		switch {
		case errors.As(err, &re):
			return nil, nil, &plan.Decline{Type: plan.PatchWontRelocate,
				Detail:     relocateDetail(rel, re),
				Determined: plan.ByNetwork}
		case err != nil:
			return nil, nil, fmt.Errorf("bump: %s: %w", rel, err)
		}
		n := res.Moved()
		if n == 0 {
			slog.Debug("patch applies where it is", "patch", rel, "hunks", len(res.Hunks))
			continue
		}
		for _, h := range res.Hunks {
			if h.Moved() {
				slog.Debug("hunk relocated", "patch", rel, "file", h.File, "hunk", h.Hunk, "from", h.OldStart, "to", h.NewStart)
			}
		}
		files = append(files, plan.FileEdit{Path: rel, Content: string(res.Bytes), Reason: hunksMoved(n)})
		moved = append(moved, name)
	}
	return files, moved, nil
}

// relocateDetail is the decline's sentence for a hunk that would not
// move: the patch, then the file and hunk as patch(1) would number it,
// then why. Composed from the error's fields rather than its text
// because the fields are the answer — a planner that had to parse them
// back out of a message could not be trusted to.
func relocateDetail(rel string, re *patch.RelocateError) string {
	d := fmt.Sprintf("%s: %s hunk #%d: %s", rel, re.File, re.Hunk, re.Reason)
	if re.Err != nil {
		d += ": " + re.Err.Error()
	}
	return d
}

// hunksMoved is the FileEdit's reason: what happened to the patch, in
// the words a renderer and a pull request body repeat.
func hunksMoved(n int) string {
	if n == 1 {
		return "1 hunk moved"
	}
	return fmt.Sprintf("%d hunks moved", n)
}

// FindingPatchesUnchecked is the kind of the finding a bump carries
// when it fetched no distfile and so had nothing to check the port's
// patches against. A constant because two packages spell it: this one
// writes it, and render prints the plan's line from its own copy, tied
// to this one by test.
const FindingPatchesUnchecked = "patches-unchecked"

// patchesUnchecked is the sentence a plan carries about the patches it
// did not relocate, because it fetched nothing to relocate them onto:
// a port recording no checksums — one fetched from a repository, say —
// has no distfile to look inside, and the relocation is only ever made
// against the source the bump just fetched.
//
// A statement, and not a proposal. It opens with its own verdict the
// way the ABI check's "unavailable" does, so a renderer can print it
// as it stands, and it carries record.Accepted for the same reason
// that finding does: nothing here is a question, and a finding still
// proposed would hold an unattended publication for an answer nobody
// can give. The count and not the names, because the names are the
// Portfile's own patchfiles line and a port can carry dozens.
func patchesUnchecked(port string, n int) record.Finding {
	noun := "patchfiles were"
	if n == 1 {
		noun = "patchfile was"
	}
	return record.Finding{
		Kind:  FindingPatchesUnchecked,
		Ports: []string{port},
		Criterion: fmt.Sprintf("patch check unavailable: %s's %d %s not checked against the new source because no distfile was fetched",
			port, n, noun),
		Disposition: record.Accepted,
	}
}
