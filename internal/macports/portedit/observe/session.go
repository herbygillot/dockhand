package observe

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/workspace"
	"github.com/herbygillot/dockhand/internal/record"
)

// ErrInconclusive means an observation could not settle what a modeled
// context depends on: host state, an unresolved read, or a boundary the
// profiles cannot close over.
var ErrInconclusive = errors.New("portedit: version probe is inconclusive")

// Session observes one Portfile's contents in modeled contexts. It knows
// the owning and selected targets, the native platform the baseline
// evaluated on, the baseline contents whose declarations it caches, and how
// to project contents into a workspace; the projections' lifetime is the
// caller's. Each observation starts its own interpreter in the projection's
// root, so a session never writes into a workspace.
type Session struct {
	Ports macports.Observer
	// Primary is the owning Portfile's target and Target the selected one;
	// a selected-only observation binds the latter.
	Primary, Target record.Target
	// Native is the platform the baseline evaluated on: what a modeled
	// context is bound with, and the native profile boundaries build on.
	Native record.Platform
	// Baseline is the contents whose declaration observations are cached.
	Baseline []byte
	// Project maps contents to the projection they are observed in: the
	// workspace itself for what it holds, an overlay otherwise.
	Project func(context.Context, []byte) (*workspace.Workspace, error)

	operands []string
	cache    map[key]macports.Observation
}

// WithBaseline is a session for other baseline contents, a stripped form of
// the Portfile for instance, with its own cache and the operands found so
// far.
func (s *Session) WithBaseline(contents []byte) *Session {
	return &Session{Ports: s.Ports, Primary: s.Primary, Target: s.Target, Native: s.Native, Baseline: contents, Project: s.Project, operands: append([]string(nil), s.operands...)}
}

// bind selects the owning Portfile, or the selected subport alone for a
// counterfactual probe, in a projection.
func (s *Session) bind(projection *workspace.Workspace, selectedOnly bool) (macports.Context, error) {
	target := s.Primary
	if selectedOnly {
		target = s.Target
	}
	return projection.Context(target, s.Native)
}
