// Package intent holds the planners: each intent type turns a desired
// end state into a plan, spending all its intelligence at plan time —
// locating spans, shadow-evaluating its own edits, fetching what must
// be fetched — so the plan it emits is complete and its prediction
// exact. Judgment of the predicted delta is the intent's own: which
// fields must move and which must not is relative to what was asked.
package intent

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
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/upstream"
)

// Bump is the intent to move a port to a new upstream version.
type Bump struct {
	Target  tree.Target
	Version string
}

// Fetcher supplies distfile checksums; the planner asks it once per new
// distfile. portfetch implements it over MacPorts' own curl — the
// planner's normal engine — and distfile.Direct in-process, for
// contexts with no installation in play.
type Fetcher interface {
	Fetch(ctx context.Context, urls []string, opts distfile.Options) (checksums.Sums, error)
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
// set. The returned error is a *Decline or *portstyle.Decline when the
// refusal is a judgment rather than a failure.
func (b Bump) Plan(ctx context.Context, ev *eval.Evaluator, fetch Fetcher) (*plan.Plan, error) {
	portdir := b.Target.Portdir
	src, err := os.ReadFile(filepath.Join(portdir, macports.PortfileName))
	if err != nil {
		return nil, err
	}
	cst, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return nil, fmt.Errorf("intent: %s: %s", portdir, errs[0].Describe(src))
	}

	before, err := ev.Snapshot(ctx, portdir, "")
	if err != nil {
		return nil, err
	}
	vals, err := contextValues(ctx, ev, portdir, b.Target.Subport, before)
	if err != nil {
		return nil, err
	}
	if vals.Version == b.Version {
		return nil, &Decline{Type: AlreadyCurrent, Detail: vals.Version}
	}

	// A vendored dependency block pins the OLD version's dependency
	// tree; bumping around it would ship a lying Portfile. Regeneration
	// is the vendor intent's job (T3).
	vendorOpts, err := ev.Options(ctx, portdir, b.Target.Subport, "", "go.vendors", "cargo.crates")
	if err != nil {
		return nil, err
	}
	for _, opt := range []string{"go.vendors", "cargo.crates"} {
		if vendorOpts[opt] != "" {
			return nil, &Decline{Type: VendoredBlock, Detail: opt}
		}
	}

	// The version edit.
	loc, err := portstyle.Locate(src, cst, vals, info.FieldVersion)
	if err != nil {
		return nil, err
	}
	slog.Debug("located version carrier", "style", loc.Style.String(), "span", loc.Span, "value", loc.Value)
	if loc.Style.Transformed() {
		return nil, &Decline{Type: TransformedStyle, Detail: loc.Style.String()}
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
		shadowDir, err := shadow(portdir, src, edits)
		if err != nil {
			return nil, err
		}
		slog.Debug("shadowed version edits", "dir", shadowDir)
		defer os.RemoveAll(shadowDir) //nolint:errcheck // temp dir cleanup

		shadowVals, err := contextTop(ctx, ev, shadowDir, b.Target.Subport)
		if err != nil {
			return nil, fmt.Errorf("intent: shadow evaluation: %w", err)
		}
		fi, err := ev.FetchInfo(ctx, shadowDir, b.Target.Subport, "", true)
		if err != nil {
			return nil, err
		}
		if len(fi.Files) == 0 {
			return nil, &Decline{Type: ChecksumsNotLocated,
				Detail: "port records checksums but fetches no distfiles"}
		}
		fetchOpts := distfile.Options{
			DisableEPSV:   fi.DisableEPSV,
			IgnoreSSLCert: fi.IgnoreSSLCert,
			UserAgent:     fi.UserAgent,
		}
		sums := make(map[string]checksums.Sums, len(fi.Files))
		for file, u := range fi.Files {
			s, err := fetch.Fetch(ctx, u, fetchOpts)
			if err != nil {
				return nil, fmt.Errorf("intent: %s: %w", file, err)
			}
			slog.Debug("fetched distfile", "file", file, "sha256", s.Sha256, "size", s.Size)
			sums[file] = s
		}
		old, err := checksums.Parse(checksumOldTokens)
		if err != nil {
			return nil, fmt.Errorf("intent: %w", err)
		}
		// Distfiles tokens may carry :tag suffixes (fetch groups); the
		// checksums block and the fetch surface both speak bare names.
		ck, err := checksumEdits(src, cst, vals.Name, old,
			bareDistfiles(vals.Distfiles), bareDistfiles(shadowVals.Distfiles), sums)
		if err != nil {
			return nil, err
		}
		edits = append(edits, ck...)
	}

	// Shadow the full edit set for the exact prediction.
	finalDir, err := shadow(portdir, src, edits)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(finalDir) //nolint:errcheck // temp dir cleanup
	after, err := ev.Snapshot(ctx, finalDir, "")
	if err != nil {
		return nil, fmt.Errorf("intent: shadow evaluation: %w", err)
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
		Subport:        b.Target.Subport,
		PortfileSHA256: plan.FileSHA256(src),
		Edits:          edits,
		Predicted:      plan.FromDelta(predicted),
	}, nil
}

// accept is the bump's judgment of its own predicted delta.
func (b Bump) accept(vals info.Values, predicted info.Delta) error {
	if len(predicted.Added) > 0 || len(predicted.Removed) > 0 {
		return &Decline{Type: SubportsChanged,
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
			info.FieldPlatforms, info.FieldDependsFetch, info.FieldDependsExtract,
			info.FieldDependsPatch, info.FieldDependsBuild, info.FieldDependsLib,
			info.FieldDependsRun, info.FieldDependsTest:
		}
	}
	if !versionReached {
		return &Decline{Type: VersionNotReached,
			Detail: fmt.Sprintf("%s would not become %s", vals.Version, b.Version)}
	}
	if len(vals.Distfiles) > 0 && !distfilesMoved {
		return &Decline{Type: FetchNotDriven,
			Detail: "distfiles unchanged by the version edit"}
	}
	if len(vals.Checksums) > 0 && !checksumsMoved {
		return &Decline{Type: FetchNotDriven, Detail: "checksums unchanged"}
	}

	for key, changes := range predicted.Changed {
		for _, ch := range changes {
			if !bumpMayChange[ch.Field] {
				return &Decline{Type: UnexpectedChange,
					Detail: fmt.Sprintf("%s: %s", key.Subport, ch.Field)}
			}
		}
	}
	return nil
}

// contextValues resolves the targeted evaluation context's values from
// a snapshot: the named subport's, or the top-level context's.
func contextValues(ctx context.Context, ev *eval.Evaluator, portdir, subport string, snap info.Snapshot) (info.Values, error) {
	if subport == "" {
		return ev.Top(ctx, portdir, "")
	}
	v, ok := snap[info.SubportKey{Subport: subport}]
	if !ok {
		return info.Values{}, fmt.Errorf("intent: %w: subport %s not in %s", tree.ErrPortNotFound, subport, portdir)
	}
	return v, nil
}

// contextTop evaluates the targeted context of a (shadow) portdir.
func contextTop(ctx context.Context, ev *eval.Evaluator, portdir, subport string) (info.Values, error) {
	if subport == "" {
		return ev.Top(ctx, portdir, "")
	}
	snap, err := ev.Snapshot(ctx, portdir, "")
	if err != nil {
		return info.Values{}, err
	}
	v, ok := snap[info.SubportKey{Subport: subport}]
	if !ok {
		return info.Values{}, fmt.Errorf("intent: subport %s not in shadow", subport)
	}
	return v, nil
}

// ResolveLatest answers what version "latest" means for a target, by
// running upstream's two-resolver check against the port's version
// carrier. A verdict that does not yield a trustworthy latest — rot,
// disagreement, no signal — declines rather than guessing.
func ResolveLatest(ctx context.Context, ev *eval.Evaluator, f *portfetch.Fetcher, target tree.Target) (string, upstream.Report, error) {
	portdir := target.Portdir
	src, err := os.ReadFile(filepath.Join(portdir, macports.PortfileName))
	if err != nil {
		return "", upstream.Report{}, err
	}
	cst, errs := syntax.Parse(src)
	if len(errs) != 0 {
		return "", upstream.Report{}, fmt.Errorf("intent: %s: %s", portdir, errs[0].Describe(src))
	}
	vals, err := contextTop(ctx, ev, portdir, target.Subport)
	if err != nil {
		return "", upstream.Report{}, err
	}
	loc, err := portstyle.Locate(src, cst, vals, info.FieldVersion)
	if err != nil {
		return "", upstream.Report{}, err
	}
	report, err := upstream.Check(ctx, ev, f, portdir, target.Subport, loc.Style)
	if err != nil {
		return "", upstream.Report{}, err
	}
	if report.Latest == "" {
		return "", report, &Decline{Type: LatestUnresolved,
			Detail: fmt.Sprintf("%s (%s)", report.Verdict, report.Detail)}
	}
	return report.Latest, report, nil
}

// bareDistfiles strips the :tag fetch-group suffixes distfiles tokens
// may carry; checksums and the fetch surface speak bare names.
func bareDistfiles(distfiles []string) []string {
	out := make([]string, 0, len(distfiles))
	for _, d := range distfiles {
		name, _, _ := strings.Cut(d, ":")
		out = append(out, name)
	}
	return out
}
