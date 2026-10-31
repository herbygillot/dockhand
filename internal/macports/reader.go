package macports

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
)

var (
	ErrStartup  = errors.New("macports: evaluator startup failed")
	ErrPlatform = errors.New("macports: evaluation requires the native platform")
	ErrTarget   = errors.New("macports: target could not be resolved")
	// ErrRefused is an evaluation that attempted an effect the evaluator
	// refuses: a write, the network, or a program not in HostPrograms.
	// Its result is not trusted, whatever the Portfile did with the refusal.
	ErrRefused = errors.New("macports: evaluation attempted a refused effect")
)

type Selection struct {
	Selector string
	Subport  string
	Variants map[string]bool
}

// Reader evaluates a selected port and resolves a selection to targets.
// Resolve may be handed a sparse workspace, one holding only what earlier
// consumers ensured: a reader materializes what it resolves, the port an
// index lookup names or the whole tree when a name needs it enumerated,
// so the target it returns can be evaluated where it was resolved.
type Reader interface {
	Evaluate(context.Context, Context) (Snapshot, error)
	Resolve(context.Context, Tree, Selection) ([]model.Target, error)
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

// SnapshotEvaluator evaluates a port whole or its selected port alone.
type SnapshotEvaluator interface {
	Evaluate(context.Context, Context) (Snapshot, error)
	EvaluateSelected(context.Context, Context) (Snapshot, error)
}

// MemoBatch is a batch that answers a port's baseline evaluation from an
// earlier one's where all it read is as it was (eval.Memo); an overlay's
// evaluation never goes through it.
type MemoBatch interface {
	Memoized() SnapshotEvaluator
}

// Batch evaluates within one interpreter until closed.
type Batch interface {
	Evaluate(context.Context, Context) (Snapshot, error)
	EvaluateSelected(context.Context, Context) (Snapshot, error)
	Close() error
}

// Resolver resolves a selection to targets, as Reader does.
type Resolver interface {
	Resolve(context.Context, Tree, Selection) ([]model.Target, error)
}

// ResolvesWithin is a reader that can resolve in a batch's interpreter,
// where the batch resolves too, rather than start one of its own.
type ResolvesWithin interface {
	Within(Batch) Resolver
}

// VersionBatch is a batch whose interpreter compares versions too, for
// the port it evaluates; closing what Versions gives leaves the batch
// open.
type VersionBatch interface {
	Versions() VersionSession
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
	NativePlatform(context.Context) (model.Platform, error)
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
