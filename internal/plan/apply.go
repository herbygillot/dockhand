package plan

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/text"
)

// ErrDrift reports that the Portfile no longer matches the plan's
// precondition: it changed between plan time and apply time, and the
// plan's edits and prediction are void.
var ErrDrift = errors.New("plan: Portfile changed since the plan was made")

// ErrMismatch reports that applying produced a delta other than the
// predicted one. The Portfile has been restored to its pre-apply bytes.
var ErrMismatch = errors.New("plan: observed delta differs from prediction")

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
	if FileSHA256(src) != p.PortfileSHA256 {
		return info.Delta{}, fmt.Errorf("%w: %s", ErrDrift, path)
	}

	after, err := ApplyEdits(src, p.Edits)
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

// ApplyEdits returns src with the edits applied, verifying each edit's
// recorded Old against the bytes it claims to replace. Every path that
// realizes edits comes through here — a planner shadowing its own edit
// set as much as Apply executing a saved plan — so an edit whose Old
// disagrees with its span cannot survive planning: it fails in the
// planner's shadow, as the planner's bug, instead of surfacing at apply
// time as a false drift blamed on the user's tree.
func ApplyEdits(src []byte, edits []Edit) ([]byte, error) {
	tedits := make([]text.Edit, 0, len(edits))
	for _, e := range edits {
		span := text.Span{Start: e.Start, End: e.End}
		if e.End > len(src) || e.Start < 0 || span.Text(src) != e.Old {
			return nil, fmt.Errorf("edit at %d..%d: recorded old text does not match the source", e.Start, e.End)
		}
		tedits = append(tedits, text.Edit{Span: span, New: []byte(e.New)})
	}
	return text.Apply(src, tedits)
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
