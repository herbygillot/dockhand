// Package procid identifies an operating-system process by its PID and
// when it started, so a PID the system reuses names another process: the
// identity coord's sessions and a provider's ownership of a prefix both
// record, neither importing the other (Codex's review of 8e314b96).
package procid

import (
	"errors"
	"os"
)

// Process identifies an operating-system process by its PID and the
// system's record of when it started, so a PID the system has reused names
// a different process.
type Process struct {
	PID   int
	Start string
}

// Liveness reports whether a process is still running as the same process.
type Liveness interface {
	Alive(Process) (bool, error)
}

// Self identifies the running process.
func Self() (Process, error) {
	start, err := Start(os.Getpid())
	if err != nil {
		return Process{}, err
	}
	return Process{PID: os.Getpid(), Start: start}, nil
}

// System is the liveness the operating system reports.
type System struct{}

// Alive reports whether the PID names a running process that started when
// the recorded one did.
func (System) Alive(p Process) (bool, error) {
	if p.PID <= 0 || p.Start == "" {
		return false, nil
	}
	start, err := Start(p.PID)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return start == p.Start, nil
}
