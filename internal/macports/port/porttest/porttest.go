// Package porttest scripts what a port answers, so what a planner
// DECIDES can be tested without a Tcl shell.
//
// A planner's tail is judgment: whether the version arrived where it
// was sent, whether the revision reached its successor, whether the
// fetch followed, whether anything moved that was not asked to. None of
// that is about MacPorts — it is about a delta — and holding it behind a
// live evaluator means it is only exercised on machines with MacPorts
// installed, which is the class of test that rots quietly because the
// job that would catch it skips instead of failing.
//
// So the oracle is scripted and the judgment is exercised. What a fake
// must never be asked to stand in for is what MacPorts actually
// evaluates: that a Portfile under a real PortGroup yields this version
// and those distfiles is eval's own tests and the intents' end-to-end
// ones, and a scripted answer there would be the test agreeing with
// itself.
package porttest

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/tree"
)

// ErrUnscripted reports a question the test did not answer. It is an
// error rather than a zero value on purpose: a snapshot is total, so an
// empty one is a claim that the Portfile defines no context, and a
// planner handed that would decline for a reason the test never meant
// to write.
var ErrUnscripted = errors.New("porttest: no answer scripted")

// Oracle answers a Handle's questions from functions the test supplies.
// A nil function is not an empty answer but an unasked question: it
// returns ErrUnscripted naming the method, so a test that reaches
// further than it scripted says so.
type Oracle struct {
	OnValues    func(ctx context.Context, portdir, subport string, variants info.VariantSet) (info.Values, error)
	OnSnapshot  func(ctx context.Context, portdir string, variants info.VariantSet) (info.Snapshot, error)
	OnSubports  func(ctx context.Context, portdir string) ([]string, error)
	OnOptions   func(ctx context.Context, portdir, subport string, variants info.VariantSet, names ...string) (map[string]string, error)
	OnFetchInfo func(ctx context.Context, portdir, subport string, variants info.VariantSet, noMirrors bool) (eval.FetchInfo, error)
}

// The fake is held to the same interface as the evaluator, so a
// signature that drifts on either side is a build failure here too.
var _ port.Oracle = (*Oracle)(nil)

// Values evaluates one context.
func (o *Oracle) Values(ctx context.Context, portdir, subport string, variants info.VariantSet) (info.Values, error) {
	if o.OnValues == nil {
		return info.Values{}, unscripted("Values", portdir, subport)
	}
	return o.OnValues(ctx, portdir, subport, variants)
}

// Snapshot evaluates every context a portdir defines.
func (o *Oracle) Snapshot(ctx context.Context, portdir string, variants info.VariantSet) (info.Snapshot, error) {
	if o.OnSnapshot == nil {
		return nil, unscripted("Snapshot", portdir, "")
	}
	return o.OnSnapshot(ctx, portdir, variants)
}

// Subports lists the subports a Portfile defines.
func (o *Oracle) Subports(ctx context.Context, portdir string) ([]string, error) {
	if o.OnSubports == nil {
		return nil, unscripted("Subports", portdir, "")
	}
	return o.OnSubports(ctx, portdir)
}

// Options reads named port options.
func (o *Oracle) Options(ctx context.Context, portdir, subport string, variants info.VariantSet, names ...string) (map[string]string, error) {
	if o.OnOptions == nil {
		return nil, unscripted("Options", portdir, subport)
	}
	return o.OnOptions(ctx, portdir, subport, variants, names...)
}

// FetchInfo reports a context's fetch surface.
func (o *Oracle) FetchInfo(ctx context.Context, portdir, subport string, variants info.VariantSet, noMirrors bool) (eval.FetchInfo, error) {
	if o.OnFetchInfo == nil {
		return eval.FetchInfo{}, unscripted("FetchInfo", portdir, subport)
	}
	return o.OnFetchInfo(ctx, portdir, subport, variants, noMirrors)
}

func unscripted(method, portdir, subport string) error {
	where := portdir
	if subport != "" {
		where += " (" + subport + ")"
	}
	return fmt.Errorf("%w: %s asked of %s", ErrUnscripted, method, where)
}

// Handle returns a handle on portdir backed by an oracle — a scripted
// one here, a live evaluator in the tests that need one. The target is
// bare because a test has no tree to resolve against, and four packages
// had written this line for themselves before it was worth naming.
func Handle(ev port.Oracle, portdir string) port.Handle {
	return port.New(tree.Target{Portdir: portdir}, ev)
}

// Shadowed answers portdir with before and every other portdir with
// after — the exact shape a planner's tail asks in, where the first
// snapshot is taken on the real portdir and the second on a shadow of
// the edited source.
//
// The shadow is identified by NOT being the original because that is
// the only thing a test can know about it: the directory is made at run
// time by the handle itself, and naming it in advance would mean the
// fake and the code under test agreeing on a path neither has seen.
func Shadowed(portdir string, before, after info.Snapshot) *Oracle {
	return &Oracle{
		OnSnapshot: func(_ context.Context, dir string, _ info.VariantSet) (info.Snapshot, error) {
			if dir == portdir {
				return before, nil
			}
			return after, nil
		},
	}
}
