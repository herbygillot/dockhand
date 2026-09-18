package macports

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
)

var (
	ErrStartup  = errors.New("macports: evaluator startup failed")
	ErrPlatform = errors.New("macports: evaluation requires the native platform")
	ErrTarget   = errors.New("macports: target could not be resolved")
)

type Selection struct {
	Selector string
	Subport  string
	Variants map[string]bool
}

type Reader interface {
	Evaluate(context.Context, Context) (Snapshot, error)
	Resolve(context.Context, Tree, Selection) ([]record.Target, error)
}

// SelectedReader evaluates only the selected port for counterfactual probes.
// Whole-Portfile validation continues to use Reader.Evaluate.
type SelectedReader interface {
	EvaluateSelected(context.Context, Context) (Snapshot, error)
}

// BatchReader opens one evaluator for many probes of the same tree, such as
// candidate versions of one Portfile, instead of starting an interpreter per probe.
type BatchReader interface {
	OpenBatch(context.Context, Tree) (Batch, error)
}

// Batch evaluates within one interpreter until closed.
type Batch interface {
	Evaluate(context.Context, Context) (Snapshot, error)
	EvaluateSelected(context.Context, Context) (Snapshot, error)
	Close() error
}

// NativeReader supplies native platform facts for locally selected source trees.
type NativeReader interface {
	Reader
	NativePlatform(context.Context) (record.Platform, error)
}

// Evaluator is the reader an edit requires: whole and selected-only
// evaluation, modeled observation of declarations and reads, and interpreter
// sessions. Every edit is judged across observed contexts, so a reader that
// cannot observe cannot edit; the one implementation is eval.Evaluator.
type Evaluator interface {
	Reader
	SelectedReader
	Observer
	BatchReader
}

// NativeEvaluator is an Evaluator that also reports the native platform.
type NativeEvaluator interface {
	Evaluator
	NativePlatform(context.Context) (record.Platform, error)
}

// Validate checks selector syntax before filesystem or evaluator access.
func (s Selection) Validate() error {
	if err := validateVariants(s.Variants); err != nil {
		return err
	}
	if s.Subport != "" && !ValidName(s.Subport) {
		return fmt.Errorf("%w: invalid subport", ErrTarget)
	}
	if !fs.ValidPath(s.Selector) || strings.ContainsAny(s.Selector, "\\\x00") || s.Selector == "." {
		return fmt.Errorf("%w: use a snapshot-relative port path or directory name", ErrTarget)
	}
	return nil
}
