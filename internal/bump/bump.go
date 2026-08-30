// Package bump plans version bumps: the flagship intent, moving a port
// to a new upstream version. It spends all its intelligence at plan
// time — locating spans, shadow-evaluating its own edits, fetching the
// new distfiles for checksums — so the plan it emits is complete and
// its prediction exact. The shared planner vocabulary (declines, the
// shadow helper, the fetcher seam) lives in internal/intent.
package bump

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/tempdir"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/herbygillot/dockhand/internal/vendored"
	"github.com/herbygillot/dockhand/internal/vendored/cargo2port"
)

// Bump is the intent to move a port to a new upstream version. The port
// it applies to is the handle Plan is given: the intent names the
// desired end state, the handle names the subject.
type Bump struct {
	Version string
}

// bumpMayChange are the fields a bump is allowed to move. Everything
// else moving is evidence the edits did more than asked, and the plan
// declines. The set is the intent's: the same field can be required
// here and forbidden for another intent.
var bumpMayChange = map[info.Field]bool{
	info.FieldVersion:   true,
	info.FieldRevision:  true,
	info.FieldDistfiles: true,
	info.FieldChecksums: true,
}

// Plan produces the complete bump plan: version and revision edits,
// checksum edits computed from the actually-fetched new distfiles, and
// the exact predicted delta from a shadow evaluation of the final edit
// set. The returned error is an *intent.Decline or *portstyle.Decline
// when the refusal is a judgment rather than a failure.
func (b Bump) Plan(ctx context.Context, h port.Handle, fetch intent.Fetcher) (*plan.Plan, error) {
	portdir := h.Target.Portdir
	src, cst, err := h.Source()
	if err != nil {
		return nil, err
	}

	before, err := h.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	vals, err := h.Values(ctx)
	if err != nil {
		return nil, err
	}
	if vals.Version == b.Version {
		return nil, &intent.Decline{Type: intent.AlreadyCurrent, Detail: vals.Version}
	}

	// A vendored dependency block pins the OLD version's dependency
	// tree; bumping around it would ship a lying Portfile. cargo.crates
	// is regenerated below, from the lockfile inside the new distfile.
	// The rest have no generator wired up yet.
	switch {
	case vals.Vendored.GoVendors != "":
		return nil, &intent.Decline{Type: intent.VendoredBlock, Detail: "go.vendors"}
	case vals.Vendored.CargoCratesGithub != "":
		return nil, &intent.Decline{Type: intent.VendoredBlock, Detail: "cargo.crates_github"}
	}
	// A patch over the lockfile means the crate set the port builds is
	// not the one upstream shipped, so regenerating from the distfile's
	// copy would state something untrue. This is judged here, before any
	// network: a refusal that needs no download should never cost one.
	if vals.Vendored.CargoCrates != "" {
		if pf, ok := patchesLockfile(vals); ok {
			return nil, &intent.Decline{Type: intent.VendoredBlock,
				Detail: fmt.Sprintf("%s rewrites %s, so the built crate set is not the one upstream shipped", pf, cargo2port.LockName)}
		}
	}

	// The version edit.
	loc, err := portstyle.Locate(src, cst, vals, info.FieldVersion)
	if err != nil {
		return nil, err
	}
	slog.Debug("located version carrier", "style", loc.Style.String(), "span", loc.Span, "value", loc.Value)
	if loc.Style.Transformed() {
		return nil, &intent.Decline{Type: intent.TransformedStyle, Detail: loc.Style.String()}
	}
	edits := []plan.Edit{{
		Start: loc.Span.Start, End: loc.Span.End,
		Old: loc.Span.Text(src), New: b.Version, Reason: "version",
	}}

	// The revision reset: a present line rewrites to 0; an absent line
	// is already 0.
	revLoc, err := portstyle.Locate(src, cst, vals, info.FieldRevision)
	var pd *portstyle.Decline
	switch {
	case err == nil:
		if revLoc.Span.Text(src) != "0" {
			edits = append(edits, plan.Edit{
				Start: revLoc.Span.Start, End: revLoc.Span.End,
				Old: revLoc.Span.Text(src), New: "0", Reason: "revision reset",
			})
		}
	case errors.As(err, &pd) && pd.Type == portstyle.UnknownStyle:
		// No revision line; nothing to reset.
	default:
		return nil, err
	}

	// Shadow the version edits to learn the new distfiles and their
	// URLs, then fetch them for checksums.
	checksumOldTokens := vals.Checksums
	if len(checksumOldTokens) > 0 {
		shadow, cleanup, err := intent.Shadow(h, src, edits)
		if err != nil {
			return nil, err
		}
		defer cleanup()
		slog.Debug("shadowed version edits", "dir", shadow.Target.Portdir)

		shadowVals, err := shadow.Values(ctx)
		if err != nil {
			return nil, fmt.Errorf("bump: shadow evaluation: %w", err)
		}
		fi, err := shadow.FetchInfo(ctx, true)
		if err != nil {
			return nil, err
		}
		if len(fi.Files) == 0 {
			return nil, &intent.Decline{Type: intent.ChecksumsNotLocated,
				Detail: "port records checksums but fetches no distfiles"}
		}

		// What a vendored block supplies is not the port's own. Those
		// distfiles and their checksums come from the block, and
		// regenerating it replaces them wholesale — so they are neither
		// fetched nor rewritten here. Subtracting them leaves what the
		// port fetches for itself: one file for most cargo ports, out of
		// two hundred.
		supplied, err := suppliedDistfiles(vals.Vendored)
		if err != nil {
			return nil, err
		}
		ownOld, err := vendored.Own(vals.Distfiles, supplied)
		if err != nil {
			return nil, err
		}
		ownNew, err := vendored.Own(shadowVals.Distfiles, supplied)
		if err != nil {
			return nil, err
		}
		if len(ownNew) == 0 {
			return nil, &intent.Decline{Type: intent.ChecksumsNotLocated,
				Detail: "every distfile comes from a vendored block"}
		}

		fetchDir, removeFetched, err := h.TempDir.MakeDir("distfiles")
		if err != nil {
			return nil, err
		}
		defer removeFetched()

		fetchOpts := distfile.Options{
			DisableEPSV:   fi.DisableEPSV,
			IgnoreSSLCert: fi.IgnoreSSLCert,
			UserAgent:     fi.UserAgent,
		}
		sums := make(map[string]checksums.Sums, len(ownNew))
		fetched := make([]string, 0, len(ownNew))
		for _, file := range ownNew {
			u, ok := fi.Files[file]
			if !ok {
				return nil, fmt.Errorf("bump: %s: the fetch surface offers no urls", file)
			}
			if file != filepath.Base(file) {
				return nil, fmt.Errorf("bump: distfile name %q is not a bare file name", file)
			}
			dest := filepath.Join(fetchDir, file)
			s, err := fetch.Fetch(ctx, u, fetchOpts, dest)
			if err != nil {
				return nil, fmt.Errorf("bump: %s: %w", file, err)
			}
			slog.Debug("fetched distfile", "file", file, "sha256", s.Sha256, "size", s.Size)
			sums[file] = s
			fetched = append(fetched, dest)
		}
		recorded, err := checksums.Parse(checksumOldTokens)
		if err != nil {
			return nil, fmt.Errorf("bump: %w", err)
		}
		ck, err := checksumEdits(src, cst, vals.Name, ownRecords(recorded, ownOld), ownOld, ownNew, sums)
		if err != nil {
			return nil, err
		}
		edits = append(edits, ck...)

		// The block is regenerated from the lockfile inside the distfile
		// just fetched, so the crate set and the checksum recorded for
		// that distfile describe the same bytes.
		if vals.Vendored.CargoCrates != "" {
			blockEdit, err := cargoBlockEdit(ctx, h.TempDir, src, cst, vals, shadowVals.Worksrcdir, fetched)
			if err != nil {
				return nil, err
			}
			edits = append(edits, blockEdit)
		}
	}

	// Shadow the full edit set for the exact prediction.
	final, cleanup, err := intent.Shadow(h, src, edits)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	after, err := final.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("bump: shadow evaluation: %w", err)
	}
	predicted := before.Diff(after)
	slog.Debug("shadow prediction", "changed", len(predicted.Changed), "added", len(predicted.Added), "removed", len(predicted.Removed))

	if err := b.accept(vals, predicted); err != nil {
		return nil, err
	}

	slices.SortFunc(edits, func(a, b plan.Edit) int { return a.Start - b.Start })
	return &plan.Plan{
		Format:         plan.Format,
		Intent:         "bump",
		Portdir:        portdir,
		Subport:        h.Target.Subport,
		PortfileSHA256: plan.FileSHA256(src),
		Edits:          edits,
		Predicted:      plan.FromDelta(predicted),
	}, nil
}

// accept is the bump's judgment of its own predicted delta.
func (b Bump) accept(vals info.Values, predicted info.Delta) error {
	if len(predicted.Added) > 0 || len(predicted.Removed) > 0 {
		return &intent.Decline{Type: intent.SubportsChanged,
			Detail: fmt.Sprintf("%d added, %d removed", len(predicted.Added), len(predicted.Removed))}
	}

	key := info.SubportKey{Subport: vals.Name}
	var versionReached, distfilesMoved, checksumsMoved bool
	for _, ch := range predicted.Changed[key] {
		switch ch.Field {
		case info.FieldVersion:
			versionReached = slices.Equal(ch.New, []string{b.Version})
		case info.FieldDistfiles:
			distfilesMoved = true
		case info.FieldChecksums:
			checksumsMoved = true
		case info.FieldName, info.FieldRevision, info.FieldEpoch,
			info.FieldCategories, info.FieldLicense, info.FieldMaintainers,
			info.FieldPlatforms, info.FieldDescription, info.FieldHomepage,
			info.FieldLongDescription,
			info.FieldDependsFetch, info.FieldDependsExtract,
			info.FieldDependsPatch, info.FieldDependsBuild, info.FieldDependsLib,
			info.FieldDependsRun, info.FieldDependsTest:
		}
	}
	if !versionReached {
		return &intent.Decline{Type: intent.VersionNotReached,
			Detail: fmt.Sprintf("%s would not become %s", vals.Version, b.Version)}
	}
	if len(vals.Distfiles) > 0 && !distfilesMoved {
		return &intent.Decline{Type: intent.FetchNotDriven,
			Detail: "distfiles unchanged by the version edit"}
	}
	if len(vals.Checksums) > 0 && !checksumsMoved {
		return &intent.Decline{Type: intent.FetchNotDriven, Detail: "checksums unchanged"}
	}

	for key, changes := range predicted.Changed {
		for _, ch := range changes {
			if !bumpMayChange[ch.Field] {
				return &intent.Decline{Type: intent.UnexpectedChange,
					Detail: fmt.Sprintf("%s: %s", key.Subport, ch.Field)}
			}
		}
	}
	return nil
}

// ResolveLatest answers what version "latest" means for a port, by
// running upstream's two-resolver check against its version carrier. A
// verdict that does not yield a trustworthy latest — rot, disagreement,
// no signal — declines rather than guessing.
func ResolveLatest(ctx context.Context, h port.Handle, f *portfetch.Fetcher) (string, upstream.Report, error) {
	src, cst, err := h.Source()
	if err != nil {
		return "", upstream.Report{}, err
	}
	vals, err := h.Values(ctx)
	if err != nil {
		return "", upstream.Report{}, err
	}
	loc, err := portstyle.Locate(src, cst, vals, info.FieldVersion)
	if err != nil {
		return "", upstream.Report{}, err
	}
	report, err := upstream.Check(ctx, h, f, loc.Style, vals.Livecheck)
	if err != nil {
		return "", upstream.Report{}, err
	}
	if report.Latest == "" {
		return "", report, &intent.Decline{Type: intent.LatestUnresolved,
			Detail: fmt.Sprintf("%s (%s)", report.Verdict, report.Detail)}
	}
	return report.Latest, report, nil
}

// suppliedDistfiles names the distfiles a port's vendored blocks
// contribute. Only cargo.crates reaches here; the other kinds decline
// before any of this runs.
func suppliedDistfiles(v info.Vendored) ([]string, error) {
	if v.CargoCrates == "" {
		return nil, nil
	}
	crates, err := cargo2port.Crates(v.CargoCrates)
	if err != nil {
		return nil, fmt.Errorf("bump: %w", err)
	}
	return cargo2port.Supplied(crates), nil
}

// ownRecords keeps the checksum records belonging to the port's own
// distfiles. A vendored block appends one record per distfile it
// supplies, and those literals live inside the block rather than in any
// checksums command — regenerating the block rewrites them, and looking
// for them among the checksums command's words would find nothing.
func ownRecords(recorded []checksums.Recorded, own []string) []checksums.Recorded {
	keep := make(map[string]bool, len(own))
	for _, n := range own {
		keep[n] = true
	}
	out := make([]checksums.Recorded, 0, len(recorded))
	for _, r := range recorded {
		// A record with no file name is the single-distfile form, which
		// only the port itself writes.
		if r.File == "" || keep[r.File] {
			out = append(out, r)
		}
	}
	return out
}

// cargoBlockEdit regenerates a cargo.crates block from the Cargo.lock
// inside the port's own distfiles.
func cargoBlockEdit(ctx context.Context, root tempdir.Root, src []byte, cst *syntax.Script, vals info.Values, worksrcdir string, fetched []string) (plan.Edit, error) {
	span, err := vendored.Locate(src, cst, portstyle.ScopeOf(src, vals.Name), cargo2port.Kind)
	if err != nil {
		return plan.Edit{}, err
	}
	lock, from, err := cargo2port.Lockfile(ctx, fetched, worksrcdir)
	if err != nil {
		return plan.Edit{}, err
	}
	slog.Debug("read lockfile", "from", filepath.Base(from), "bytes", len(lock))
	block, err := cargo2port.Generate(ctx, root, lock)
	if err != nil {
		return plan.Edit{}, err
	}
	slog.Debug("regenerated block", "kind", cargo2port.Kind.String(), "bytes", len(block))
	return vendored.Edit(src, span, block, cargo2port.Kind), nil
}

// patchesLockfile reports whether any of the port's patchfiles rewrites
// the file the generator reads. The patches' own diff headers are read
// rather than their names guessed at: a patch named for something else
// can still touch the lockfile. A patchfile that cannot be read is
// treated as touching it — the point is to prove the lockfile is
// untouched, and an unreadable patch proves nothing.
func patchesLockfile(vals info.Values) (string, bool) {
	for _, pf := range vals.Patchfiles {
		body, err := os.ReadFile(filepath.Join(vals.Filespath, pf))
		if err != nil {
			return pf, true
		}
		for line := range strings.Lines(string(body)) {
			if !strings.HasPrefix(line, "--- ") && !strings.HasPrefix(line, "+++ ") {
				continue
			}
			if strings.Contains(line, cargo2port.LockName) {
				return pf, true
			}
		}
	}
	return "", false
}
