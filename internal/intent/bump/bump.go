// Package bump plans version bumps: the flagship intent, moving a port
// to a new upstream version. It spends all its intelligence at plan
// time — locating spans, shadow-evaluating its own edits, fetching the
// new distfiles for checksums — so the plan it emits is complete and
// its prediction exact. The vocabulary it speaks at its boundaries
// lives where each piece belongs: declines and edit realization in
// plan, the fetcher seam in distfile, and the shape and tail every
// intent shares in the parent package.
//
// What is here is what only a bump knows — locating the version
// carrier and proving it drives the value, subtracting what a vendored
// block supplies, deciding what must have moved for a bump to have
// happened. The apply-shadow-diff-guard-assemble tail it used to end
// with is intent.Finish.
package bump

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/text"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/upstream"
	"github.com/herbygillot/dockhand/internal/vendored"
	"github.com/herbygillot/dockhand/internal/vendored/families"
)

// Bump is the intent to move a port to a new upstream version. The port
// it applies to is the handle Plan is given: the intent names the
// desired end state, the handle names the subject.
//
// The assertion is the catalogue's shape (D20) made mechanical: every
// intent is an intent.Planner, and one that drifts fails to build.
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
	// Tools resolves what a plan may have to run beyond the evaluator:
	// the vendored-block generators and the archiver that reads a
	// lockfile or go.mod out of a distfile. The command hands in the
	// run's finder; a planner that reads no distfile never touches it.
	Tools *tool.Finder
	// ClosesTicket is the Trac ticket this bump closes, bound for the
	// minted commit's trailer. It changes nothing about what is planned.
	ClosesTicket string
	// Riders is the run's rider policy, carried here so that the two
	// already-current declines below can name what they held back with
	// them.
	Riders intent.RiderPolicy
	// Dependents are what the tree's reverse index says depends on this
	// port, carried through to the instruction-comment finding rule.
	Dependents []string
	// Frames evaluates this port under another platform frame, so a
	// checksums command in a branch this host did not take can be asked
	// what it would actually fetch. Nil is a road with no evaluator pool
	// to spend, and tracksVersion degrades to the refusal rather than to
	// silence. See intent.Params.Frames.
	Frames framer
}

var _ intent.Planner = Bump{}

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
	// write is what goes into the span, which is the target itself for a
	// carrier whose literal IS the version and only a PART of it for one
	// that computes the version from its literal.
	write, viaSet, proven := b.Version, false, false

	// DISCOVERY IS ASKED FIRST AND OF EVERY ROAD, including a
	// corroborated one, because corroboration is a claim about TEXT and
	// this is a claim about CAUSE. An obsolete-stub branch that
	// hard-codes the version a sibling subport computes corroborates
	// perfectly and drives nothing; only substituting a value and
	// watching where it lands can tell the two apart. It costs one
	// evaluation and it is purely additive — a carrier it cannot prove
	// falls through to the roads below unchanged.
	if found, ok := discover(ctx, h, src, cst, vals, b.Version); ok {
		carrier, style, write, viaSet, proven, exact = found.Span, found.Style, found.Write, found.ViaSet, true, true
		slog.Debug("version carrier proven by substitution", "style", style.String(),
			"span", carrier, "literal", carrier.Text(src), "template", found.Template, "write", write)
	} else if lerr == nil {
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
			// Nothing in the text to write into and nothing proven by
			// substitution: the run ends here, so the last thing worth
			// spending an evaluation on is telling the user WHERE the
			// version comes from instead of that it is computed.
			pd.Elsewhere = elsewhere(ctx, h, src, cst, vals)
			return nil, pd
		}
		carrier, style, exact = cand.Span, cand.Style, false
	}
	if !proven {
		slog.Debug("version carrier", "style", style.String(), "span", carrier, "literal", carrier.Text(src), "corroborated", lerr == nil)
	}

	// Whether the version is moving governs more than one decision
	// below, so it is named once here — in carrier vocabulary, which is
	// the target's own.
	moving := carrier.Text(src) != write
	if !moving && !b.Force {
		// The port's own version and not the carrier's literal, which for
		// a computed carrier is a FRAGMENT: "already in the desired
		// state: 2" is a sentence about nothing a person asked for.
		return nil, &plan.Decline{Type: plan.AlreadyCurrent, Detail: vals.Version,
			Withheld: intent.Withheld(src, cst, b.Riders)}
	}

	// A vendored dependency block pins the OLD version's dependency
	// tree; bumping around it would ship a lying Portfile. Each family
	// judges its own honesty here, before any network is spent.
	for _, r := range regenerators {
		if !r.Present(vals) {
			continue
		}
		if reason, veto := r.Veto(vals); veto {
			return nil, &plan.Decline{Type: plan.VendoredBlock, Detail: reason}
		}
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

	// An uncorroborated carrier must prove itself before anything is
	// planned on it: write the target into the candidate, shadow,
	// re-evaluate, and demand the version moved. The corroboration rule
	// extended one step — from "text equals value" to "text demonstrably
	// drives value" — at the cost of one evaluation, only on this path.
	if lerr != nil && moving && !proven {
		if err := probeCarrier(ctx, h, src, carrier, b.Version, vals.Version); err != nil {
			slog.Debug("counterfactual probe failed", "span", carrier, "err", err)
			var pd *portstyle.Decline
			if errors.As(lerr, &pd) && pd.Type == portstyle.NotLiteral {
				// The candidate the text offered does not drive the
				// version. Whether the real carrier is a transform in
				// this file or a name from outside it is the question
				// the user is about to ask, so answer it here.
				pd.Elsewhere = elsewhere(ctx, h, src, cst, vals)
				return nil, pd
			}
			return nil, lerr
		}
		slog.Debug("carrier proven by counterfactual", "style", style.String(), "span", carrier)
	}

	// A forced re-derivation exists to catch a stealth update: fetch the
	// distfiles again and see whether they still hash to what the
	// Portfile records. A port that records no checksums has nothing to
	// compare, and with the version staying put there is nothing else
	// downstream to re-derive — no fetch, no vendored block, no declared
	// toolchain floor, since all three hang off the recorded sums. So
	// this run cannot produce a plan that claims anything, and the
	// honest answer is the one the tail's witness rule asks an intent
	// for: decline first, in the port's own terms, rather than let a
	// refusal about dockhand's falsifiability rule reach the user in the
	// failure band.
	if !moving && len(vals.Checksums) == 0 {
		return nil, &plan.Decline{Type: plan.AlreadyCurrent,
			Detail:   fmt.Sprintf("%s records no checksums, so a re-derivation at %s has nothing to fetch and nothing to compare", vals.Name, b.Version),
			Withheld: intent.Withheld(src, cst, b.Riders)}
	}

	// What this run spent, named for the case where the shadow ends up
	// showing nothing at all. A bump that fetched holds the strongest
	// kind of evidence — bytes off the network, hashed — and a bump that
	// only rewrote the carrier still did something the shadow can
	// contradict, which accept below then says in the intent's own
	// words. The third case, a forced run that would spend neither, is
	// the decline above and never reaches the tail.
	witness := intent.NoWitness
	if moving {
		witness = intent.WitnessVersionWritten
	}

	// The version edit.
	var edits []edit.Edit
	checksumsViaSet := false
	if moving {
		edits = append(edits, edit.Edit{
			Kind:  edit.Version,
			Start: carrier.Start, End: carrier.End,
			Old: carrier.Text(src), New: write, Reason: "version",
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
					Kind:  edit.RevisionReset,
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

	// The port's patches, relocated onto the new source: the whole
	// files the plan writes beside the Portfile, and the names of the
	// patchfiles that moved, for the summary. Both stay nil on every
	// path that fetches nothing — a port recording no checksums has no
	// distfile to look inside, so its patches are left as they are —
	// and unchecked stays true there, for the sentence the plan then
	// carries about them. Left as they are is not the answer "checked,
	// and still where they were", and a plan that said nothing would
	// be read as giving the second.
	var files []plan.FileEdit
	var refreshed []string
	unchecked := len(vals.Patchfiles) > 0
	var patchFindings []plan.Finding

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
		rc := vendored.Regen{
			Src: src, CST: cst,
			Handle: h, Vals: vals,
			Shadow: shadow, ShadowVals: shadowVals,
			TempDir: h.TempDir,
			Fetch:   fetch,
			Tools:   b.Tools,
		}
		var supplied []string
		for _, r := range regenerators {
			if !r.Present(vals) {
				continue
			}
			sup, serr := r.Supplied(ctx, rc)
			if serr != nil {
				// A family speaks for its block and names it; which intent
				// was asking is this package's to say, and saying it here
				// is what keeps one prefix per message.
				return nil, fmt.Errorf("bump: %w", serr)
			}
			supplied = append(supplied, sup...)
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
		// The fetch supersedes the carrier: bytes off the network are the
		// stronger evidence, and both fields it could name are in
		// bumpMayChange either way. One kind is declared and not a set,
		// because what the empty-delta rule needs is a reason the
		// prediction is silent, and the strongest one is the honest one.
		witness = intent.WitnessFetched
		recorded, err := checksums.Parse(checksumOldTokens)
		if err != nil {
			return nil, fmt.Errorf("bump: %w", err)
		}
		ck, viaSet, err := checksumEdits(src, cst, vals.Name, checksums.ForFiles(recorded, ownOld), ownOld, ownNew, sums)
		if err != nil {
			return nil, err
		}
		checksumsViaSet = viaSet
		edits = append(edits, ck...)

		// The patches the new version applies, moved onto the source
		// just fetched. The shadow's list and not the original's: a
		// patchfile conditional on the version is applied or not by
		// the version the port is going to, and that evaluation is the
		// shadow's. This is not a rider, and --no-riders does not
		// reach it. A rider is housekeeping dockhand offers on the side
		// of a change and the user may refuse; a patch that no longer
		// applies is the bump itself not being complete, so the policy
		// is not consulted, and a patch that will not relocate declines
		// the bump outright rather than shipping a branch whose patch
		// phase would fail.
		var unresolved []plan.Finding
		files, refreshed, unresolved, err = relocatePatches(ctx, b.Tools, h.Target.Portdir, shadowVals, shadowVals.Worksrcdir, fetched)
		if err != nil {
			return nil, err
		}
		unchecked = false
		// A PATCH THAT WOULD NOT COME OVER IS A QUESTION, NOT THE END OF
		// THE BUMP (ruled 8 September 2026). It used to decline the whole
		// plan; the branch is minted now, carrying the patches that DID
		// relocate and a Proposed finding naming each that did not, so a
		// person can resolve it on the branch and the machine cannot
		// publish past it. See FindingPatchUnrelocated.
		//
		// The relocations that succeeded are kept rather than discarded
		// with the failure: each patch relocates independently of the
		// others, and throwing away correct work would leave the person
		// redoing a move dockhand already made.
		patchFindings = append(patchFindings, unresolved...)

		// Each present family regenerates its block for the target — the
		// crate set and the checksum recorded for the distfile describe
		// the same bytes, because both came from this fetch.
		rc.Fetched = fetched
		for _, r := range regenerators {
			if !r.Present(vals) {
				continue
			}
			blockEdits, rerr := r.Regenerate(ctx, rc)
			if rerr != nil {
				return nil, rerr
			}
			edits = append(edits, blockEdits...)
		}
		// The declared Go floor follows the new go.mod when its series
		// moved — update-only; a port that never declared one is never
		// gated by dockhand either.
		if tc, ok, err := toolchainMinEdit(ctx, b.Tools, src, cst, vals.Name, fetched, shadowVals.Worksrcdir); err != nil {
			return nil, err
		} else if ok {
			edits = append(edits, tc)
		}
	}

	// The commit's subject. A refreshed patch is named in it because a
	// reviewer reading the subject alone should know the change touched
	// more than the Portfile, and the name is what they would look for
	// in the diff.
	summary := fmt.Sprintf("%s: update to %s", vals.Name, b.Version)
	if len(refreshed) > 0 {
		summary += ", refresh " + strings.Join(refreshed, ", ")
	}

	// Everything from here — shadow the full edit set, diff it, refuse
	// what the prediction did not promise, fold in the riders, assemble
	// — is the tail every intent runs, and it is written once. What is
	// left in this package is what only a bump knows: which spans to
	// rewrite, and what has to have moved for a bump to have happened.
	p, err := intent.Finish(ctx, h, src, edits,
		intent.Identity{
			Intent:       "bump",
			Slug:         vals.Name + "-" + b.Version,
			Summary:      summary,
			ClosesTicket: b.ClosesTicket,
		},
		intent.FinishOpts{
			Before:    before,
			Vals:      vals,
			CST:       cst,
			MayChange: bumpMayChange,
			Accept: func(predicted info.Delta) error {
				if err := b.accept(vals, predicted, moving, exact); err != nil {
					return err
				}
				// A DISTFILE THIS EVALUATION COULD NOT FETCH MUST NOT BE
				// LEFT DESCRIBING THE OLD RELEASE. The two shapes are
				// asked separately because only one of them is visible
				// here: a renamed file with unmoved digests is IN the
				// prediction, and a checksums command in a branch this
				// host did not take is not in it at all.
				if err := intent.ChecksumsFollowTheirFiles(predicted, vals.Name); err != nil {
					return err
				}
				if left := intent.UnreachedChecksums(src, cst, vals.Name, edits); len(left) > 0 {
					// Only a block that would GO STALE is a defect. A branch
					// pinning its own older release — cliclick holds 4.0.1
					// for the systems 5.x dropped — is correctly untouched
					// by this bump, and refusing it would refuse the port
					// for doing the right thing.
					if frame, stale := staleElsewhere(ctx, b.Frames, hereFetch(ctx, h, vals), src, edits); stale {
						return &plan.Decline{Type: plan.ChecksumsUnreached,
							Detail: fmt.Sprintf("the checksums command at line %d was not reached by this evaluation, and on %s this edit moves what the port fetches",
								intent.LineOf(src, left[0].Start), frame)}
					}
				}
				// A CARRIER IN A `set` IS JUSTIFIED BY ONE CONTEXT'S
				// EVALUATION while the variable it writes may be read by
				// siblings — a top-level `set ver` behind five subports
				// moves all five. The guard is the one this package
				// already applies to a checksum located the same way, for
				// the identical reason, and the total shadow is what makes
				// it answerable.
				if viaSet {
					return intent.ViaSetIsolated(predicted, vals.Name)
				}
				return nil
			},
			ViaSet:     checksumsViaSet,
			Riders:     b.Riders,
			Witness:    witness,
			Dependents: b.Dependents,
		})
	if err != nil {
		return nil, err
	}
	// The refreshed patches ride on the plan beside the Portfile's
	// edits. They are set here and not handed to the tail because the
	// tail proves the Portfile — shadows it, diffs it, judges the delta
	// — and a whole file is outside that proof: the source it was
	// relocated against is the witness, and every realization writes
	// it as it arrives.
	p.Files = files
	// And the sentence about the patches it did not relocate, where it
	// fetched nothing to relocate them onto. It is a finding — the note's
	// vocabulary for what a change noticed beyond itself — and a
	// statement rather than a question: an ABI check that could not be
	// made says "unavailable" in the same shape, and nothing here is for
	// a person to answer, so it must not hold the machine gate the way
	// a proposal does. The count is the current evaluation's, since this
	// path shadowed nothing before the tail and the list the sentence
	// refers to is the one in the Portfile a reader has in front of them.
	if unchecked {
		p.Findings = append(p.Findings, patchesUnchecked(vals.Name, len(vals.Patchfiles)))
	}
	p.Findings = append(p.Findings, patchFindings...)
	return p, nil
}

// accept is the bump's judgment of its own predicted delta — the half
// of the old tail that is genuinely this intent's. The structural
// questions it used to ask first and last, that no context appeared or
// vanished and that nothing outside bumpMayChange moved, are Finish's
// now and identical for every intent.
//
// moving and exact are the carrier's terms: moving says the carrier
// literal was rewritten, and exact says carrier and evaluated
// vocabulary coincide — when they do not (a transform sits between),
// the evaluated version's new value is the Portfile's business, and
// movement is the requirement.
func (b Bump) accept(vals info.Values, predicted info.Delta, moving, exact bool) error {
	var versionChanged, versionReached, distfilesMoved, checksumsMoved bool
	for _, ch := range intent.OwnChanges(predicted, vals.Name) {
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
	//
	// Every refusal below reads a shadow prediction: the Portfile with
	// the version edit applied, evaluated. What a fetch returned is not
	// in it — this judgment is made before any distfile is looked at.
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
		Kind:  edit.Version,
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
// no signal — declines rather than guessing. tools resolves the git
// the forge's tags are read with.
//
// m is the politeness the witnesses are consulted under, and it passes
// straight through. Its zero value is one port asking one question,
// which is what this has always been; a selector-scale bump hands in a
// paced, caching one, because resolving "latest" for a thousand ports
// is a thousand questions for one forge and nothing else about this
// road knows that.
func ResolveLatest(ctx context.Context, tools *tool.Finder, h port.Handle, f *portfetch.Fetcher, gh upstream.GhRunner, m upstream.Manners) (string, upstream.Report, error) {
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
	report, err := upstream.Check(ctx, tools, h, f, style, vals.Livecheck, gh, m)
	if err != nil {
		return "", upstream.Report{}, err
	}
	if report.Latest == "" {
		// One decline, two bands. What a user reads is the same sentence
		// either way — the decline words it — and what a script reads is
		// not: witnesses that produced nothing usable are upstream's
		// problem, while a newest version dockhand judged unfit to act on
		// is dockhand's own refusal. The verdict is what tells them
		// apart, and it does not survive being formatted into Detail.
		return "", report, upstream.Unresolved(report.Verdict, &plan.Decline{Type: plan.LatestUnresolved,
			Detail: fmt.Sprintf("%s (%s)", report.Verdict, report.Detail)})
	}
	return report.Latest, report, nil
}

// regenerators is the registry of vendored-block families — the closed
// list bump consults instead of an if-chain. It lives in
// vendored/families now that a refresh consults the same list: a
// registry owned by one intent is a registry the other has to reach
// into. What families adds beyond the list is the translation, so a
// family's refusal arrives here already wearing the plan's vocabulary.
var regenerators = families.All()

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
