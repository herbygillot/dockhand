package coord

import "github.com/herbygillot/dockhand/internal/procid"

// Process, Liveness, Self, and System are procid's: a process by its PID
// and start time, which a provider's prefix ownership records too.
type (
	Process  = procid.Process
	Liveness = procid.Liveness
	System   = procid.System
)

// Self identifies the running process.
func Self() (Process, error) { return procid.Self() }
