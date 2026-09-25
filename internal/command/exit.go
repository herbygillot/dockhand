package command

import "fmt"

// ExitError carries the exit code a command's outcome calls for (Design
// v3 §12): 2 for a failed check, 3 for attention needed, 130 for an
// interrupt.
type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string { return e.Message }

// ExitCode is the process exit code for err: its own when it carries one,
// else 1.
func ExitCode(err error) int {
	if exit, ok := err.(*ExitError); ok {
		return exit.Code
	}
	return 1
}

func exitf(code int, format string, args ...any) error {
	return &ExitError{Code: code, Message: fmt.Sprintf(format, args...)}
}
