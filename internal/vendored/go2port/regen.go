package go2port

import (
	"context"
	"log/slog"
	"strings"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/vendored"
)

// Blocks is the go family's Regenerator: go.vendors recomputed by the
// tool for the target version's own go.mod.
type Blocks struct{}

var _ vendored.Regenerator = Blocks{}

func (Blocks) Kind() vendored.Kind { return Kind }

func (Blocks) Present(vals info.Values) bool { return vals.Vendored.GoVendors != "" }

// Veto never refuses: a bump moves the version, and the version is what
// go2port needs to recompute the block from the module's own go.mod at
// the new ref. Everything that could still go wrong is discovered with
// the shadow in hand, and declines from Regenerate.
func (Blocks) Veto(info.Values) (string, bool) { return "", false }

// VetoRefresh always refuses, and this is the family's own reason
// rather than the refreshing intent's — which is the point of asking
// here. The block records a sha256 per module, computed by go2port from
// bytes a refresh has no version move to go and fetch. Rewriting only
// the port's own sums would leave every module line pinning whatever
// was true before, on a Portfile that now claims to be current.
func (Blocks) VetoRefresh(info.Values) (string, bool) {
	return "go.vendors pins module bytes a refresh never fetches, so the block would go on vouching for what upstream replaced", true
}

// Supplied inverts rather than reimplements: the vendor distfile
// naming lives deep in the golang PortGroup (per-forge rules in
// go._translate_package_id), so the port's own distfile —
// ${distname}${extract.suffix}, both evaluated — is subtracted and
// everything else in the context's distfiles is the block's.
func (Blocks) Supplied(ctx context.Context, rc vendored.Regen) ([]string, error) {
	opts, err := rc.Handle.Options(ctx, "distname", "extract.suffix")
	if err != nil {
		return nil, err
	}
	own := opts["distname"] + opts["extract.suffix"]
	var supplied []string
	for _, d := range rc.Vals.Distfiles {
		name, _, _ := strings.Cut(d, ":")
		if name != own {
			supplied = append(supplied, name)
		}
	}
	return supplied, nil
}

// Regenerate asks the SHADOW — the edited Portfile at the new version —
// so both answers are the portgroup's own composition for the target:
// go.package as go.setup derived it, and git.branch as the resolved
// git ref (tag prefix and suffix already applied by github.setup and
// its siblings). Measured: handing go2port a bare version against a
// v-prefixed tag makes it emit a portfile with no vendors block at
// all rather than fail.
func (Blocks) Regenerate(ctx context.Context, rc vendored.Regen) ([]edit.Edit, error) {
	span, err := vendored.Locate(rc.Src, rc.CST, portstyle.ScopeOf(rc.Src, rc.Vals.Name), Kind)
	if err != nil {
		return nil, err
	}
	opts, err := rc.Shadow.Options(ctx, "go.package", "git.branch")
	if err != nil {
		return nil, err
	}
	pkg := strings.TrimSpace(opts["go.package"])
	ref := strings.TrimSpace(opts["git.branch"])
	if pkg == "" || ref == "" {
		return nil, &vendored.Decline{Kind: Kind,
			Detail: "go.vendors present but go.package or git.branch is empty; the module ref is unknowable"}
	}
	slog.Debug("regenerating go.vendors", "package", pkg, "ref", ref)
	block, err := Generate(ctx, rc.Tools, pkg, ref)
	if err != nil {
		return nil, err
	}
	slog.Debug("regenerated block", "kind", Kind.String(), "bytes", len(block))
	return []edit.Edit{vendored.Edit(rc.Src, span, block, Kind)}, nil
}
