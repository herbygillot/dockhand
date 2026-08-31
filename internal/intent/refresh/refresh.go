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
	"os"
	"path/filepath"
	"slices"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/checksums/rewrite"
	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/vendored"
	"github.com/herbygillot/dockhand/internal/vendored/cargo2port"
)

// Refresh is the intent to make a port's recorded checksums true again.
type Refresh struct{}

var _ plan.Planner = Refresh{}

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
func (Refresh) Plan(ctx context.Context, h port.Handle, fetch distfile.Fetcher) (*plan.Plan, error) {
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
	// go.vendors ports decline as bump's do: their sums live in a block
	// no one regenerates yet. cargo.crates ports proceed — the block's
	// records are subtracted below, and refreshing the port's own
	// distfile does not touch the crates.
	if vals.Vendored.GoVendors != "" {
		return nil, &plan.Decline{Type: plan.VendoredBlock, Detail: "go.vendors"}
	}

	supplied, err := cargo2port.SuppliedIn(vals.Vendored.CargoCrates)
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
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
	fetchDir, err := os.MkdirTemp("", "dockhand-refresh-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(fetchDir) //nolint:errcheck // temp dir cleanup

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
	edits, unlocated := rewrite.Edits(src, cst, portstyle.ScopeOf(src, vals.Name), reps)
	for _, u := range unlocated {
		// Unlike a bump there are no renames here, so every replacement
		// is a checksum value and every value must be found: one that is
		// not written literally cannot be made true by editing.
		return nil, &plan.Decline{Type: plan.ChecksumsNotLocated,
			Detail: fmt.Sprintf("recorded value %q not found as a literal (%s)", u.Old, u.Reason)}
	}
	if len(edits) == 0 {
		return nil, &plan.Decline{Type: plan.AlreadyCurrent,
			Detail: "recorded checksums match what upstream serves"}
	}

	// Shadow the edits for the exact prediction, as every intent does:
	// the plan states what will change, and applying it holds the
	// change to that statement.
	edited, err := plan.ApplyEdits(src, edits)
	if err != nil {
		return nil, err
	}
	shadow, cleanup, err := h.Shadow(edited)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	after, err := shadow.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("refresh: shadow evaluation: %w", err)
	}
	predicted := before.Diff(after)
	if err := accept(vals, predicted); err != nil {
		return nil, err
	}

	slices.SortFunc(edits, func(a, b plan.Edit) int { return a.Start - b.Start })
	return &plan.Plan{
		Format:         plan.Format,
		Intent:         "refresh-checksums",
		Portdir:        h.Target.Portdir,
		Subport:        h.Target.Subport,
		PortfileSHA256: plan.FileSHA256(src),
		Edits:          edits,
		Predicted:      plan.FromDelta(predicted),
	}, nil
}

// accept is the refresh's judgment of its own predicted delta: the
// checksums moved, and nothing else did. The version staying put is the
// intent's defining property — a version that moves under a "refresh"
// is some other change wearing this one's name.
func accept(vals info.Values, predicted info.Delta) error {
	if len(predicted.Added) > 0 || len(predicted.Removed) > 0 {
		return &plan.Decline{Type: plan.SubportsChanged,
			Detail: fmt.Sprintf("%d added, %d removed", len(predicted.Added), len(predicted.Removed))}
	}
	key := info.SubportKey{Subport: vals.Name}
	var checksumsMoved bool
	for _, ch := range predicted.Changed[key] {
		if ch.Field == info.FieldChecksums {
			checksumsMoved = true
		}
	}
	if !checksumsMoved {
		return &plan.Decline{Type: plan.FetchNotDriven,
			Detail: "the edits moved no checksums"}
	}
	for key, changes := range predicted.Changed {
		for _, ch := range changes {
			if !refreshMayChange[ch.Field] {
				return &plan.Decline{Type: plan.UnexpectedChange,
					Detail: fmt.Sprintf("%s: %s", key.Subport, ch.Field)}
			}
		}
	}
	return nil
}
