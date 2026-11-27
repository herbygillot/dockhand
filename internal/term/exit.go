package term

import (
	"errors"
	"fmt"
	"io"

	"github.com/herbygillot/dockhand/internal/refusal"
)

// The exit codes a command's outcome calls for (Design v3 §12), named so
// no site chooses a bare number and none reads one back as control flow.
const (
	// ExitFailure is an error, a refusal among them.
	ExitFailure = 1
	// ExitAttention is an outcome that needs a person's look: a hold, a
	// look left incomplete, or archives that don't match.
	ExitAttention = 3
)

// ExitError carries the exit code a command's outcome calls for.
type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string { return e.Message }

// ExitCode is the process exit code for err: its own when it carries one,
// however wrapped, else its refusal's kind's (refusal.Kind.Exit, the one
// table of them), else ExitFailure.
func ExitCode(err error) int {
	if exit := new(ExitError); errors.As(err, &exit) {
		return exit.Code
	}
	if r := refusal.Of(err); r != nil {
		return r.Kind().Exit()
	}
	return ExitFailure
}

// Exitf is an ExitError with code, its message formatted.
func Exitf(code int, format string, args ...any) error {
	return &ExitError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// WriteError writes a command's final error, as the process ends on it,
// made safe to show (Visible): it may carry another's words, Git's,
// GitHub's, or a remote's, which mustn't draw over the terminal (#330,
// #365). An error with no words writes nothing.
func WriteError(w io.Writer, err error) {
	if err == nil || err.Error() == "" {
		return
	}
	fmt.Fprintln(w, Visible(err.Error()))
}
