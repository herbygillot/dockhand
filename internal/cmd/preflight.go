package cmd

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

// knownFailOn asks, before any VM boots, whether a portdir declares
// known_fail under a release's platform frame — mpbb's own layering,
// borrowed: the buildbot excludes known_fail ports at list time by
// reading the evaluated option, and only falls back to discovering it
// mid-build. The evaluation runs host-side under eval.WithPlatform
// (one short-lived session, about a second) against the portdir given,
// which for a branch verification is the materialized branch content —
// the known_fail under test is the branch's, not the checkout's.
func knownFailOn(ctx context.Context, rs *runstate.Context, portdir string, r platform.Release) (bool, error) {
	pfx, err := rs.Prefix()
	if err != nil {
		return false, err
	}
	frame := info.Platform{OS: "macosx", Major: r.Darwin, Arch: "arm"}
	p, err := pool.New(ctx, pfx, 1, eval.WithPlatform(frame))
	if err != nil {
		return false, err
	}
	defer p.Close()
	h := port.New(tree.Target{Portdir: portdir}, p.Evaluators()[0])
	opts, err := h.Options(ctx, "known_fail")
	if err != nil {
		return false, err
	}
	return tclTrue(opts["known_fail"]), nil
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
