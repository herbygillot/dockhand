package plan

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
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

// Apply executes the plan: verify the precondition hash, write the
// edits, re-evaluate, and demand the observed delta equal the predicted
// one exactly. On mismatch the original bytes are restored — a plan
// either does precisely what it said or does nothing.
//
// The returned delta is the observed one, for reporting; on success it
// equals the prediction by construction.
func (p *Plan) Apply(ctx context.Context, ev *eval.Evaluator) (info.Delta, error) {
	path := filepath.Join(p.Portdir, macports.PortfileName)
	src, err := os.ReadFile(path)
	if err != nil {
		return info.Delta{}, err
	}
	after, err := p.Materialize(src)
	if errors.Is(err, ErrDrift) {
		return info.Delta{}, fmt.Errorf("%w: %s", ErrDrift, path)
	}
	if err != nil {
		// The precondition hash already matched, so a mismatch here is a
		// corrupt plan rather than a moved file — but either way the
		// plan's claims and the world disagree, which is drift's meaning.
		return info.Delta{}, fmt.Errorf("%w: %w", ErrDrift, err)
	}

	before, err := ev.Snapshot(ctx, p.Portdir, "")
	if err != nil {
		return info.Delta{}, err
	}
	if err := writeFile(path, after); err != nil {
		return info.Delta{}, err
	}
	now, err := ev.Snapshot(ctx, p.Portdir, "")
	if err != nil {
		// The edited file does not even evaluate: restore.
		if rerr := writeFile(path, src); rerr != nil {
			return info.Delta{}, errors.Join(err, rerr)
		}
		return info.Delta{}, fmt.Errorf("%w: edited Portfile failed to evaluate: %w", ErrMismatch, err)
	}
	observed := before.Diff(now)
	slog.Debug("apply observed delta", "changed", len(observed.Changed))
	if !reflect.DeepEqual(FromDelta(observed), p.Predicted) {
		if rerr := writeFile(path, src); rerr != nil {
			return observed, errors.Join(fmt.Errorf("%w (and restore failed)", ErrMismatch), rerr)
		}
		return observed, ErrMismatch
	}
	return observed, nil
}

// writeFile replaces the Portfile atomically: temp file in the same
// directory, then rename.
func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".dockhand-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()         //nolint:errcheck // best-effort on the error path
		_ = os.Remove(name) //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}
