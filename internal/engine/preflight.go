package engine

import (
	"context"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/platform"
	"github.com/herbygillot/dockhand/internal/verdict"
)

// preflightOn asks, before any VM boots, whether a portdir declares
// known_fail under a release's platform frame — mpbb's own layering,
// borrowed: the buildbot excludes known_fail ports at list time by
// reading the evaluated option, and only falls back to discovering it
// mid-build. The evaluation runs host-side under eval.WithPlatform
// (one short-lived session, about a second) against the portdir given,
// which for a branch verification is the materialized branch content —
// the known_fail under test is the branch's, not the checkout's.
// The answer is verdict's own Preflight: what the evaluation found is
// a fact the scheduling judgment weighs, and there is nothing here to
// hold it that verdict does not already declare.
//
// The session is this function's own and not the run's: the frame is
// the TARGET release's, and the run's evaluator carries the host's.
func (e *Engine) preflightOn(ctx context.Context, portdir string, r platform.Release) (verdict.Preflight, error) {
	frame := info.Platform{OS: "macosx", Major: r.Darwin, Arch: "arm"}
	ev, err := e.Session(ctx, eval.WithPlatform(frame))
	if err != nil {
		return verdict.Preflight{}, err
	}
	defer ev.Close()
	h := port.New(tree.Target{Portdir: portdir}, ev)
	opts, err := h.Options(ctx, "known_fail", "use_xcode")
	if err != nil {
		return verdict.Preflight{}, err
	}
	return verdict.Preflight{
		KnownFail: tclTrue(opts["known_fail"]),
		UseXcode:  tclTrue(opts["use_xcode"]),
	}, nil
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
