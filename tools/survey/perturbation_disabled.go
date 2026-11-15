//go:build !surveymeasure

package main

import (
	"context"

	"github.com/herbygillot/dockhand/internal/macports"
)

func perturbContext(context.Context, macports.Observer, macports.Snapshot, macports.PortInfo, *toleranceCapture) *probeResult {
	return &probeResult{Status: "failed", ErrorCode: "probe-build-required"}
}
