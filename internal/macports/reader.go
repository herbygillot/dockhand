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
	// ErrUnanswered is an evaluation in a modelled context that a closed
	// answer ended: a call the model has no answer for, which an empty Mac
	// answers as missing, and the Portfile didn't catch. The context is
	// inconclusive, never failed (UnansweredError).
	ErrUnanswered = errors.New("macports: the modelled context has no answer for a call the evaluation needed")
)

// UnansweredError names the call a modelled context had no answer for,
// whose closed answer ended the evaluation.
type UnansweredError struct{ Call string }

func (e *UnansweredError) Error() string {
	return fmt.Sprintf("the modelled context has no answer for `%s`, which the evaluation needed", e.Call)
}

func (e *UnansweredError) Unwrap() error { return ErrUnanswered }

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

// Batch evaluates within one interpreter until closed.
type Batch interface {
	Evaluate(context.Context, Context) (Snapshot, error)
	EvaluateSelected(context.Context, Context) (Snapshot, error)
	Close() error
}

// Linter runs Base's lint target on a selected port on this Mac, as the
// guest's port lint does, with --nitpick where asked: what it warned of
// and what it called an error, each line once, without its prefix. It
// runs through the evaluator's dispatcher, as an evaluation does, so a
// Portfile's refused effect is an error. Dependencies, conflicts, and
// replaced_by aren't looked up in an index, which an evaluation's tree
// hasn't: what it says is of the Portfile's own lines and options.
type Linter interface {
	Lint(ctx context.Context, source Context, nitpick bool) (Lint, error)
}

// Lint is what a lint run said: the warnings and errors, each line once.
type Lint struct {
	Warnings, Errors []string
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
