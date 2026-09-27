//go:build !darwin && !linux

package command

import "github.com/herbygillot/dockhand/internal/engine"

// startCleanup has no detached process to start here; serve cleans up.
var startCleanup = func(engine.Options) error { return nil }
