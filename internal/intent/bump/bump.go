// Package bump plans version bumps: the flagship intent, moving a port
// to a new upstream version. It spends all its intelligence at plan
// time — locating spans, shadow-evaluating its own edits, fetching the
// new distfiles for checksums — so the plan it emits is complete and
// its prediction exact. The vocabulary it speaks at its boundaries
// lives where each piece belongs: declines and edit realization in
// plan, the fetcher seam in distfile. internal/intent is a namespace,
// not a package — it holds the intents and nothing else.
package bump

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/tempdir"
	"github.com/herbygillot/dockhand/internal/text"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/herbygillot/dockhand/internal/vendored"
	"github.com/herbygillot/dockhand/internal/vendored/cargo2port"
	"github.com/herbygillot/dockhand/internal/vendored/go2port"
)

// Bump is the intent to move a port to a new upstream version. The port
// it applies to is the handle Plan is given: the intent names the
// desired end state, the handle names the subject.
//
// The assertion is the catalogue's shape (D20) made mechanical: every
// intent is a plan.Planner, and one that drifts fails to build.
type Bump struct {
	Version string
	// Force plans a bump to a version the port already carries. The
	// point is not to rewrite the version — that edit is skipped, since
	// it would change nothing — but to re-derive everything downstream
	// of it: the distfile is fetched again and its checksums compared,
	// and a vendored block is regenerated from the lockfile inside it.
	// It is how a stealth update is caught, where an upstream re-rolled
	// a release at the same version and the same URL.
	Force bool
}

var _ plan.Planner = Bump{}

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
// set. The returned error is an *plan.Decline or *portstyle.Decline
// when the refusal is a judgment rather than a failure.
func (b Bump) Plan(ctx context.Context, h port.Handle, fetch distfile.Fetcher) (*plan.Plan, error) {
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

	// The version carrier, and the vocabulary decision (D22): the
	// target is written verbatim into the carrier span, because targets
	// come from upstream — livecheck, forge tags — which speaks the
	// carrier's vocabulary. When the carrier corroborates untransformed,
	// literal and evaluated coincide and exact acceptance applies. A
	// registered transform (perl5) or a failed corroboration with a
	// literal candidate (an ad hoc Portfile transform) still bumps: the
	// evaluated version is derived by the shadow evaluation, never
	// computed by an inverse, and acceptance checks movement instead of
	// equality.
	loc, lerr := portstyle.Locate(src, cst, vals, info.FieldVersion)
	carrier, style, exact := loc.Span, loc.Style, true
	if lerr == nil {
		if loc.Style.Transformed() {
			exact = false
		}
	} else {
		var pd *portstyle.Decline
		if !errors.As(lerr, &pd) || pd.Type != portstyle.NotLiteral {
			return nil, lerr
		}
		cand, ok := lastLiteralCandidate(pd.Candidates)
		if !ok {
			return nil, lerr
		}
		carrier, style, exact = cand.Span, cand.Style, false
	}
	slog.Debug("version carrier", "style", style.String(), "span", carrier, "literal", carrier.Text(src), "corroborated", lerr == nil)

	// Whether the version is moving governs more than one decision
	// below, so it is named once here — in carrier vocabulary, which is
	// the target's own.
	moving := carrier.Text(src) != b.Version
	if !moving && !b.Force {
		return nil, &plan.Decline{Type: plan.AlreadyCurrent, Detail: carrier.Text(src)}
	}

	// A vendored dependency block pins the OLD version's dependency
	// tree; bumping around it would ship a lying Portfile. cargo.crates
	// is regenerated below, from the lockfile inside the new distfile.
	// The rest have no generator wired up yet.
	switch {
	case vals.Vendored.CargoCratesGithub != "":
		return nil, &plan.Decline{Type: plan.VendoredBlock, Detail: "cargo.crates_github"}
	}
	// Zig dependency sets are hand-vendored — no MacPorts option marks
	// them, so the Portfile itself is read for Zig's package-hash shape.
	// The stake is higher than a missing generator: without this
	// refusal a bump re-checksums the OLD pinned dependency commits and
	// produces a branch that looks complete and is wrong — the one
	// behaviour the tool promises against. Field-measured on ziggity.
	if h := zigVendorHash(src); h != "" {
		return nil, &plan.Decline{Type: plan.VendoredBlock,
			Detail: fmt.Sprintf("hand-vendored Zig dependency set (build.zig.zon package hash %s); dockhand cannot re-resolve the pinned dependencies yet", h)}
	}
	// A patch over the lockfile means the crate set the port builds is
	// not the one upstream shipped, so regenerating from the distfile's
	// copy would state something untrue. This is judged here, before any
	// network: a refusal that needs no download should never cost one.
	if vals.Vendored.CargoCrates != "" {
		if pf, ok := patchesLockfile(vals); ok {
			return nil, &plan.Decline{Type: plan.VendoredBlock,
				Detail: fmt.Sprintf("%s rewrites %s, so the built crate set is not the one upstream shipped", pf, cargo2port.LockName)}
		}
	}

	// An uncorroborated carrier must prove itself before anything is
	// planned on it: write the target into the candidate, shadow,
	// re-evaluate, and demand the version moved. The corroboration rule
	// extended one step — from "text equals value" to "text demonstrably
	// drives value" — at the cost of one evaluation, only on this path.
	if lerr != nil && moving {
		if err := probeCarrier(ctx, h, src, carrier, b.Version, vals.Version); err != nil {
			slog.Debug("counterfactual probe failed", "span", carrier, "err", err)
			return nil, lerr
		}
		slog.Debug("carrier proven by counterfactual", "style", style.String(), "span", carrier)
	}

	// The version edit.
	var edits []edit.Edit
	if me, ok := modelineEdit(src); ok {
		edits = append(edits, me)
	}
	if moving {
		edits = append(edits, edit.Edit{
			Start: carrier.Start, End: carrier.End,
			Old: carrier.Text(src), New: b.Version, Reason: "version",
		})

		// The revision reset: a present line rewrites to 0; an absent
		// line is already 0. It belongs to a version that moved. Resetting
		// it where the version stayed would not merely be pointless: a
		// port at revision 2 reset to 0 goes backwards in MacPorts'
		// ordering, so installations already holding it would decline the
		// very update the run was made to deliver.
		revLoc, err := portstyle.Locate(src, cst, vals, info.FieldRevision)
		var pd *portstyle.Decline
		switch {
		case err == nil:
			if revLoc.Span.Text(src) != "0" {
				edits = append(edits, edit.Edit{
					Start: revLoc.Span.Start, End: revLoc.Span.End,
					Old: revLoc.Span.Text(src), New: "0", Reason: "revision reset",
				})
			}
		case errors.As(err, &pd) && pd.Type == portstyle.UnknownStyle:
			// No revision line; nothing to reset.
		default:
			return nil, err
		}
	}

	// Shadow the version edits to learn the new distfiles and their
	// URLs, then fetch them for checksums.
	checksumOldTokens := vals.Checksums
	if len(checksumOldTokens) > 0 {
		edited, err := edit.Apply(src, edits)
		if err != nil {
			return nil, err
		}
		shadow, cleanup, err := h.Shadow(edited)
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
			return nil, &plan.Decline{Type: plan.ChecksumsNotLocated,
				Detail: "port records checksums but fetches no distfiles"}
		}

		// What a vendored block supplies is not the port's own. Those
		// distfiles and their checksums come from the block, and
		// regenerating it replaces them wholesale — so they are neither
		// fetched nor rewritten here. Subtracting them leaves what the
		// port fetches for itself: one file for most cargo ports, out of
		// two hundred.
		supplied, err := suppliedDistfiles(ctx, h, vals.Vendored, vals.Distfiles)
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
			return nil, &plan.Decline{Type: plan.ChecksumsNotLocated,
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
		ck, err := checksumEdits(src, cst, vals.Name, checksums.ForFiles(recorded, ownOld), ownOld, ownNew, sums)
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
		if vals.Vendored.GoVendors != "" {
			blockEdit, err := goBlockEdit(ctx, shadow, src, cst, vals)
			if err != nil {
				return nil, err
			}
			edits = append(edits, blockEdit)
		}
		// The declared Go floor follows the new go.mod when its series
		// moved — update-only; a port that never declared one is never
		// gated by dockhand either.
		if tc, ok, err := toolchainMinEdit(ctx, src, cst, vals.Name, fetched, shadowVals.Worksrcdir); err != nil {
			return nil, err
		} else if ok {
			edits = append(edits, tc)
		}
	}

	// Shadow the full edit set for the exact prediction.
	finalSrc, err := edit.Apply(src, edits)
	if err != nil {
		return nil, err
	}
	final, cleanup, err := h.Shadow(finalSrc)
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

	if err := b.accept(vals, predicted, moving, exact); err != nil {
		return nil, err
	}

	slices.SortFunc(edits, func(a, b edit.Edit) int { return a.Start - b.Start })
	return &plan.Plan{
		Format:         plan.Format,
		Intent:         "bump",
		Port:           vals.Name,
		Slug:           vals.Name + "-" + b.Version,
		Summary:        fmt.Sprintf("%s: update to %s", vals.Name, b.Version),
		Portdir:        portdir,
		Subport:        h.Target.Subport,
		PortfileSHA256: edit.FileSHA256(src),
		Edits:          edits,
		Predicted:      plan.FromDelta(predicted),
	}, nil
}

// accept is the bump's judgment of its own predicted delta. moving and
// exact are the carrier's terms: moving says the carrier literal was
// rewritten, and exact says carrier and evaluated vocabulary coincide —
// when they do not (a transform sits between), the evaluated version's
// new value is the Portfile's business, and movement is the
// requirement.
func (b Bump) accept(vals info.Values, predicted info.Delta, moving, exact bool) error {
	if len(predicted.Added) > 0 || len(predicted.Removed) > 0 {
		return &plan.Decline{Type: plan.SubportsChanged,
			Detail: fmt.Sprintf("%d added, %d removed", len(predicted.Added), len(predicted.Removed))}
	}

	key := info.SubportKey{Subport: vals.Name}
	var versionChanged, versionReached, distfilesMoved, checksumsMoved bool
	for _, ch := range predicted.Changed[key] {
		switch ch.Field {
		case info.FieldVersion:
			versionChanged = true
			if exact {
				versionReached = slices.Equal(ch.New, []string{b.Version})
			} else {
				versionReached = len(ch.New) == 1 && ch.New[0] != ""
			}
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
	// These three rules exist to prove a bump did what a bump does: the
	// version arrived where it was sent, and the fetch surface followed
	// it. A forced re-derivation at the version the port already carries
	// is judged by the opposite standard — the version must not move,
	// and whether anything downstream moved is the finding rather than
	// the requirement, since an upstream that re-rolled nothing is the
	// ordinary case.
	if moving {
		if !versionReached {
			return &plan.Decline{Type: plan.TargetNotReached,
				Detail: fmt.Sprintf("%s would not become %s", vals.Version, b.Version)}
		}
		if len(vals.Distfiles) > 0 && !distfilesMoved {
			return &plan.Decline{Type: plan.FetchNotDriven,
				Detail: "distfiles unchanged by the version edit"}
		}
		if len(vals.Checksums) > 0 && !checksumsMoved {
			return &plan.Decline{Type: plan.FetchNotDriven, Detail: "checksums unchanged"}
		}
	} else if versionChanged {
		return &plan.Decline{Type: plan.UnexpectedChange,
			Detail: fmt.Sprintf("version moved from %s during a re-derivation at the same version", vals.Version)}
	}

	for key, changes := range predicted.Changed {
		for _, ch := range changes {
			if !bumpMayChange[ch.Field] {
				return &plan.Decline{Type: plan.UnexpectedChange,
					Detail: fmt.Sprintf("%s: %s", key.Subport, ch.Field)}
			}
		}
	}
	return nil
}

// lastLiteralCandidate picks the probe's carrier: the last literal
// candidate in document order, matching Tcl's later-assignment-wins —
// the same preference corroboration itself uses. A candidate that is
// not a plain literal cannot be rewritten, so it cannot carry.
func lastLiteralCandidate(cands []portstyle.Candidate) (portstyle.Candidate, bool) {
	var best portstyle.Candidate
	ok := false
	for _, c := range cands {
		if c.Literal && (!ok || c.Span.Start > best.Span.Start) {
			best, ok = c, true
		}
	}
	return best, ok
}

// probeCarrier proves a span drives the version, by observation: the
// target written into the span must move the evaluated version. The
// Portfile transforms its carrier through Tcl of its own — a [string
// map] over github.version, say — and no registry of Go transforms can
// chase that family, so the proof is empirical, exactly as the
// corroboration rule demands: evaluation is the only authority on what
// a Portfile means.
func probeCarrier(ctx context.Context, h port.Handle, src []byte, span text.Span, target, current string) error {
	probed, err := edit.Apply(src, []edit.Edit{{
		Start: span.Start, End: span.End,
		Old: span.Text(src), New: target, Reason: "version",
	}})
	if err != nil {
		return err
	}
	shadow, cleanup, err := h.Shadow(probed)
	if err != nil {
		return err
	}
	defer cleanup()
	sv, err := shadow.Values(ctx)
	if err != nil {
		return fmt.Errorf("bump: probe evaluation: %w", err)
	}
	if sv.Version == current || sv.Version == "" {
		return fmt.Errorf("bump: writing %s into the candidate moved nothing", target)
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
	style := loc.Style
	if err != nil {
		// Latest resolution needs the style family, not a proven span:
		// livecheck and the forge speak the carrier's vocabulary either
		// way, and the bump that follows proves the span by
		// counterfactual before writing anything.
		var pd *portstyle.Decline
		if !errors.As(err, &pd) || pd.Type != portstyle.NotLiteral {
			return "", upstream.Report{}, err
		}
		cand, ok := lastLiteralCandidate(pd.Candidates)
		if !ok {
			return "", upstream.Report{}, err
		}
		style = cand.Style
	}
	report, err := upstream.Check(ctx, h, f, style, vals.Livecheck)
	if err != nil {
		return "", upstream.Report{}, err
	}
	if report.Latest == "" {
		return "", report, &plan.Decline{Type: plan.LatestUnresolved,
			Detail: fmt.Sprintf("%s (%s)", report.Verdict, report.Detail)}
	}
	return report.Latest, report, nil
}

// suppliedDistfiles names the distfiles a port's vendored blocks
// contribute — the set the checksum machinery must not treat as the
// port's own.
func suppliedDistfiles(ctx context.Context, h port.Handle, v info.Vendored, distfiles []string) ([]string, error) {
	switch {
	case v.CargoCrates != "":
		supplied, err := cargo2port.SuppliedIn(v.CargoCrates)
		if err != nil {
			return nil, fmt.Errorf("bump: %w", err)
		}
		return supplied, nil
	case v.GoVendors != "":
		// The vendor distfile naming lives deep in the golang
		// PortGroup (go._translate_package_id, per-forge rules);
		// reimplementing it would be reimplementing MacPorts. Inverted
		// instead: the port's own distfile is ${distname}${extract.suffix},
		// both evaluated, and everything else the context's distfiles
		// list carries is the block's contribution.
		opts, err := h.Options(ctx, "distname", "extract.suffix")
		if err != nil {
			return nil, err
		}
		own := opts["distname"] + opts["extract.suffix"]
		var supplied []string
		for _, d := range distfiles {
			name, _, _ := strings.Cut(d, ":")
			if name != own {
				supplied = append(supplied, name)
			}
		}
		return supplied, nil
	}
	return nil, nil
}

// goBlockEdit regenerates a go.vendors block for the target version.
// Unlike cargo's, the regeneration needs no distfile: go2port resolves
// the module's go.mod at that version from its forge and downloads
// every dependency to checksum it — the same network a maintainer
// running the tool by hand spends. The module path comes from the
// port's own evaluated go.package, so what is regenerated is what the
// golang PortGroup will read.
func goBlockEdit(ctx context.Context, shadow port.Handle, src []byte, cst *syntax.Script, vals info.Values) (edit.Edit, error) {
	span, err := vendored.Locate(src, cst, portstyle.ScopeOf(src, vals.Name), go2port.Kind)
	if err != nil {
		return edit.Edit{}, err
	}
	// Asked of the SHADOW — the edited Portfile at the new version — so
	// both answers are the portgroup's own composition for the target:
	// go.package as go.setup derived it, and git.branch as the resolved
	// git ref (tag prefix and suffix already applied by github.setup and
	// its siblings). Measured: handing go2port a bare version against a
	// v-prefixed tag makes it emit a portfile with no vendors block at
	// all rather than fail.
	opts, err := shadow.Options(ctx, "go.package", "git.branch")
	if err != nil {
		return edit.Edit{}, err
	}
	pkg := strings.TrimSpace(opts["go.package"])
	ref := strings.TrimSpace(opts["git.branch"])
	if pkg == "" || ref == "" {
		return edit.Edit{}, &plan.Decline{Type: plan.VendoredBlock,
			Detail: "go.vendors present but go.package or git.branch is empty; the module ref is unknowable"}
	}
	slog.Debug("regenerating go.vendors", "package", pkg, "ref", ref)
	block, err := go2port.Generate(ctx, pkg, ref)
	if err != nil {
		return edit.Edit{}, err
	}
	slog.Debug("regenerated block", "kind", go2port.Kind.String(), "bytes", len(block))
	return vendored.Edit(src, span, block, go2port.Kind), nil
}

// cargoBlockEdit regenerates a cargo.crates block from the Cargo.lock
// inside the port's own distfiles.
func cargoBlockEdit(ctx context.Context, root tempdir.Root, src []byte, cst *syntax.Script, vals info.Values, worksrcdir string, fetched []string) (edit.Edit, error) {
	span, err := vendored.Locate(src, cst, portstyle.ScopeOf(src, vals.Name), cargo2port.Kind)
	if err != nil {
		return edit.Edit{}, err
	}
	lock, from, err := cargo2port.Lockfile(ctx, fetched, worksrcdir)
	if err != nil {
		return edit.Edit{}, err
	}
	slog.Debug("read lockfile", "from", filepath.Base(from), "bytes", len(lock))
	// The regenerated block is re-laid under the existing block's own
	// measured geometry — but only a geometry Assess proved by
	// re-rendering the existing block byte for byte. Unchanged crates
	// then render identical whatever wrote the original, a hand script
	// included; an unproven geometry keeps the tool's output verbatim.
	geom, proven := cargo2port.Assess(span.Text(src))
	slog.Debug("assessed block layout", "layout", string(geom.Layout), "proven", proven)
	block, err := cargo2port.Generate(ctx, root, lock, geom.Layout)
	if err != nil {
		return edit.Edit{}, err
	}
	if proven {
		block = cargo2port.Reformat(block, geom)
	}
	slog.Debug("regenerated block", "kind", cargo2port.Kind.String(), "bytes", len(block))
	return vendored.Edit(src, span, block, cargo2port.Kind), nil
}

// Modeline is the editor header the MacPorts best-practices page
// prescribes; a bump adds it to a Portfile that opens without one.
const Modeline = "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4"

// modelineEdit inserts the modeline when the Portfile's very first
// line is not one. Detection is deliberately loose — any leading
// comment carrying an emacs -*- block or a vim: stanza counts — so an
// existing modeline in either dialect, however configured, is never
// second-guessed or rewritten.
func modelineEdit(src []byte) (edit.Edit, bool) {
	first, _, _ := bytes.Cut(src, []byte("\n"))
	if bytes.HasPrefix(first, []byte("#")) &&
		(bytes.Contains(first, []byte("-*-")) || bytes.Contains(first, []byte("vim:")) || bytes.Contains(first, []byte("vi:"))) {
		return edit.Edit{}, false
	}
	return edit.Edit{Start: 0, End: 0, Old: "", New: Modeline + "\n", Reason: "modeline"}, true
}

// zigPackageHash is the shape of a Zig 0.12+ package hash as ports pin
// them: name, semver, and a long base64url fingerprint, e.g.
// vaxis-0.6.0-BWNV_HHwCQB451KS7A8SMykALblPmGwHnzSfiJHjN3_9. The
// fingerprint's length is what keeps distnames and versions from
// matching. "zig" must also appear in the Portfile, so an improbable
// literal elsewhere cannot decline an unrelated port.
var zigPackageHash = regexp.MustCompile(`\b[A-Za-z0-9_.]+-[0-9]+\.[0-9]+\.[0-9]+-[A-Za-z0-9_-]{30,}\b`)

// zigVendorHash reports the first Zig package hash pinned in a
// Portfile, "" when none.
func zigVendorHash(src []byte) string {
	if !bytes.Contains(src, []byte("zig")) {
		return ""
	}
	return string(zigPackageHash.Find(src))
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
