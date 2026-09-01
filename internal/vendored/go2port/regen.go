package go2port

import (
	"context"
	"log/slog"
	"strings"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/vendored"
)

// Blocks is the go family's Regenerator: go.vendors recomputed by the
// tool for the target version's own go.mod.
type Blocks struct{}

var _ vendored.Regenerator = Blocks{}

func (Blocks) Kind() vendored.Kind { return Kind }

func (Blocks) Present(vals info.Values) bool { return vals.Vendored.GoVendors != "" }

func (Blocks) Veto(info.Values) (string, bool) { return "", false }

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
func (Blocks) Regenerate(ctx context.Context, rc vendored.Regen) (edit.Edit, error) {
	span, err := vendored.Locate(rc.Src, rc.CST, portstyle.ScopeOf(rc.Src, rc.Vals.Name), Kind)
	if err != nil {
		return edit.Edit{}, err
	}
	opts, err := rc.Shadow.Options(ctx, "go.package", "git.branch")
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
	block, err := Generate(ctx, pkg, ref)
	if err != nil {
		return edit.Edit{}, err
	}
	slog.Debug("regenerated block", "kind", Kind.String(), "bytes", len(block))
	return vendored.Edit(rc.Src, span, block, Kind), nil
}
