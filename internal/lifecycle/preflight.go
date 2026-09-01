package lifecycle

import (
	"context"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/eval/pool"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/runstate"
)

// preflightOn asks, before any VM boots, whether a portdir declares
// known_fail under a release's platform frame — mpbb's own layering,
// borrowed: the buildbot excludes known_fail ports at list time by
// reading the evaluated option, and only falls back to discovering it
// mid-build. The evaluation runs host-side under eval.WithPlatform
// (one short-lived session, about a second) against the portdir given,
// which for a branch verification is the materialized branch content —
// the known_fail under test is the branch's, not the checkout's.
func preflightOn(ctx context.Context, rs *runstate.Context, portdir string, r platform.Release) (preflight, error) {
	pfx, err := rs.Prefix()
	if err != nil {
		return preflight{}, err
	}
	frame := info.Platform{OS: "macosx", Major: r.Darwin, Arch: "arm"}
	p, err := pool.New(ctx, pfx, 1, eval.WithPlatform(frame))
	if err != nil {
		return preflight{}, err
	}
	defer p.Close()
	h := port.New(tree.Target{Portdir: portdir}, p.Evaluators()[0])
	opts, err := h.Options(ctx, "known_fail", "use_xcode")
	if err != nil {
		return preflight{}, err
	}
	return preflight{
		KnownFail: tclTrue(opts["known_fail"]),
		UseXcode:  tclTrue(opts["use_xcode"]),
	}, nil
}

// preflight is what one evaluation answers before any VM boots: does
// the port refuse this platform, and does it need a capability —
// full Xcode — a base may not have. Field-measured cost of not asking
// the second question: a guaranteed-failure build spent a VM slot,
// kept its worker as a debug environment nobody needed, and read as
// "this bump is broken" when the base was the limitation.
type preflight struct {
	KnownFail bool
	UseXcode  bool
}

// tclTrue reads a value the way Tcl's [string is true] does, which is
// how mpbb judges known_fail.
func tclTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
