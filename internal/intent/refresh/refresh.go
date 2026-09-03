// Package refresh plans checksum refreshes: bringing a port's recorded
// checksums back into agreement with the bytes its unchanged version
// actually serves. Two lines change and the tier is T0, and it is still
// the most suspicious intent in the catalogue — a checksum that changes
// for an unchanged version means upstream re-rolled the artifact, which
// is a supply-chain event wearing maintenance's clothes. The intent is
// therefore non-autonomous by construction: it plans and applies when a
// person asks, and the summary says plainly that someone should
// establish why before the change goes anywhere public.
//
// It is also the second intent, and deliberately shaped unlike the
// first. A refresh has no version edit, so it needs no portstyle
// location at all — a port whose version is computed, which bump
// declines as NotLiteral, refreshes fine. What the two share is what
// internal/checksums and checksums/rewrite were split for: value
// computation there, literal location there, policy here.
package refresh

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/checksums/rewrite"
	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/vendored"
	"github.com/herbygillot/dockhand/internal/vendored/families"
)

// Refresh is the intent to make a port's recorded checksums true again.
type Refresh struct {
	// ClosesTicket is the Trac ticket this repair closes, bound for the
	// minted commit's trailer.
	ClosesTicket string
	// Riders is the run's rider policy, carried here so that the
	// already-current decline below can name what it held back with it.
	Riders intent.RiderPolicy
}

var _ intent.Planner = Refresh{}

// refreshMayChange is the whole of what a refresh may move. The version
// staying put is not listed because it is enforced the other way: any
// field outside this set moving is an unexpected change, the version
// included.
var refreshMayChange = map[info.Field]bool{
	info.FieldChecksums: true,
}

// Plan produces the refresh plan: fetch what the port's own distfiles
// serve today, and where the recorded values disagree, edit them to
// what is true. The returned error is an *plan.Decline when the
// refusal is a judgment rather than a failure.
func (r Refresh) Plan(ctx context.Context, h port.Handle, fetch distfile.Fetcher) (*plan.Plan, error) {
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
	if len(vals.Checksums) == 0 {
		return nil, &plan.Decline{Type: plan.ChecksumsNotLocated,
			Detail: "port records no checksums to refresh"}
	}
	// Each block the port carries answers for itself, in its own words:
	// whether the port's own sums can move while the block stays put,
	// and which distfiles the block supplies so the refresh leaves them
	// alone. Asking the family is the point — it is what knows what its
	// block pins and what a stale line in it would claim. A refresh has
	// no version move to hang a regeneration on, so a family that says
	// no is refusing outright, where a bump would regenerate instead.
	//
	// Nothing here is fetched yet, so Regen carries only what Supplied
	// reads: the source, the context and its evaluated state.
	rc := vendored.Regen{Src: src, CST: cst, Handle: h, Vals: vals}
	var supplied []string
	for _, r := range families.All() {
		if !r.Present(vals) {
			continue
		}
		if reason, veto := r.VetoRefresh(vals); veto {
			return nil, &plan.Decline{Type: plan.VendoredBlock, Detail: reason}
		}
		sup, err := r.Supplied(ctx, rc)
		if err != nil {
			// A family speaks for its block and names it; which intent was
			// asking is this package's to say.
			return nil, fmt.Errorf("refresh: %w", err)
		}
		supplied = append(supplied, sup...)
	}
	own, err := vendored.Own(vals.Distfiles, supplied)
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}
	if len(own) == 0 {
		return nil, &plan.Decline{Type: plan.ChecksumsNotLocated,
			Detail: "every distfile comes from a vendored block"}
	}

	// No mirrors, and this is the intent where that switch is the whole
	// game: the MacPorts mirrors hold the bytes the recorded checksums
	// were made from, so a fetch that may fall back to them can answer
	// "unchanged" about an upstream that re-rolled. The question a
	// refresh asks is what upstream serves NOW, and only upstream can
	// answer it.
	fi, err := h.FetchInfo(ctx, true)
	if err != nil {
		return nil, err
	}
	fetchDir, removeFetched, err := h.TempDir.MakeDir("distfiles")
	if err != nil {
		return nil, err
	}
	defer removeFetched()

	opts := distfile.Options{
		DisableEPSV:   fi.DisableEPSV,
		IgnoreSSLCert: fi.IgnoreSSLCert,
		UserAgent:     fi.UserAgent,
	}
	sums := make(map[string]checksums.Sums, len(own))
	for _, file := range own {
		u, ok := fi.Files[file]
		if !ok {
			return nil, fmt.Errorf("refresh: %s: the fetch surface offers no urls", file)
		}
		s, err := fetch.Fetch(ctx, u, opts, filepath.Join(fetchDir, file))
		if err != nil {
			return nil, fmt.Errorf("refresh: %s: %w", file, err)
		}
		slog.Debug("fetched distfile", "file", file, "sha256", s.Sha256, "size", s.Size)
		sums[file] = s
	}

	recorded, err := checksums.Parse(vals.Checksums)
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}
	reps, err := checksums.Replacements(checksums.ForFiles(recorded, own), sums)
	if err != nil {
		return nil, &plan.Decline{Type: plan.ChecksumsNotLocated, Detail: err.Error()}
	}
	edits, unlocated, viaSet := rewrite.Edits(src, cst, portstyle.ScopeOf(src, vals.Name), vals.Name, reps)
	for _, u := range unlocated {
		// Unlike a bump there are no renames here, so every replacement
		// is a checksum value and every value must be found: one that is
		// not written literally cannot be made true by editing.
		return nil, &plan.Decline{Type: plan.ChecksumsNotLocated,
			Detail: fmt.Sprintf("recorded value %q not found as a literal (%s)", u.Old, u.Reason)}
	}
	if len(edits) == 0 {
		return nil, &plan.Decline{Type: plan.AlreadyCurrent,
			Detail:   "recorded checksums match what upstream serves",
			Withheld: intent.Withheld(src, cst, r.Riders)}
	}

	// The tail is intent.Finish, as it is for every intent: shadow the
	// edits, diff, refuse what the prediction did not promise, assemble.
	// The plan states what will change, and applying it holds the change
	// to that statement.
	//
	// Riders ride here too, on the run's policy and not on this intent's
	// opinion. The opinion was a real one and is worth stating, because
	// it is the reason --no-riders exists: a refresh touching two
	// checksum values shows a reviewer two checksum values, at the one
	// moment they are most owed an undistracted diff. What that argument
	// could not survive was being an intent's private rule — a reviewer
	// who wants the narrow diff wants it from every verb, and a
	// housekeeping rule nobody can turn on for a refresh is a rule that
	// silently never runs on two thirds of the catalogue.
	return intent.Finish(ctx, h, src, edits,
		intent.Identity{
			Intent:       "refresh-checksums",
			Slug:         vals.Name + "-checksums",
			Summary:      vals.Name + ": update checksums",
			ClosesTicket: r.ClosesTicket,
		},
		intent.FinishOpts{
			Before:    before,
			Vals:      vals,
			CST:       cst,
			MayChange: refreshMayChange,
			Accept: func(predicted info.Delta) error {
				return accept(vals, predicted)
			},
			ViaSet: viaSet,
			Riders: r.Riders,
			// The bytes are the witness, and this intent has the best kind:
			// a fetch that deliberately refused the mirrors, so what was
			// hashed is what upstream serves right now. A prediction that
			// shows nothing then means the edits missed their target, which
			// accept says below in its own words.
			Witness: "the port's distfiles were fetched from upstream and hashed",
		})
}

// accept is the refresh's judgment of its own predicted delta: the
// checksums moved. That nothing else moved is Finish's question, asked
// against refreshMayChange — and it is how the version staying put is
// enforced, which is this intent's defining property. A version that
// moves under a "refresh" is some other change wearing this one's name.
func accept(vals info.Values, predicted info.Delta) error {
	for _, ch := range intent.OwnChanges(predicted, vals.Name) {
		if ch.Field == info.FieldChecksums {
			return nil
		}
	}
	return &plan.Decline{Type: plan.FetchNotDriven,
		Detail: "the edits moved no checksums"}
}
