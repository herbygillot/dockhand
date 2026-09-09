package plan

import (
	"errors"

	"github.com/herbygillot/dockhand/internal/edit"
)

// ErrDrift reports that the Portfile no longer matches the plan's
// precondition: it changed between plan time and apply time, and the
// plan's edits and prediction are void.
var ErrDrift = errors.New("plan: Portfile changed since the plan was made")

// ErrMismatch reports that applying produced a delta other than the
// predicted one. The Portfile has been restored to its pre-apply bytes.
var ErrMismatch = errors.New("plan: observed delta differs from prediction")

// Materialize is the one home for the plan's precondition: src must
// hash to PortfileSHA256, and only then are the edits applied to it.
// Every realization — the in-place apply below, the commit minted from
// a base blob, the gate's shadow — computes its bytes here, so the
// verdict one of them reaches transfers to the others by content
// identity rather than by three hand-copied sequences agreeing.
//
// A hash miss returns ErrDrift bare: each caller names what drifted
// (the file's path, the branch, the portdir) in its own words, and a
// suffix added here would sit inside every one of those messages. An
// apply failure after the hash matched is returned as edit.Apply
// produced it; whether that counts as drift is the caller's call.
func (p *Plan) Materialize(src []byte) ([]byte, error) {
	if edit.FileSHA256(src) != p.PortfileSHA256 {
		return nil, ErrDrift
	}
	return edit.Apply(src, p.Edits)
}
