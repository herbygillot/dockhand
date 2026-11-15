//go:build !surveymeasure

package main

import (
	"context"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

func perturbContext(context.Context, macports.Observer, model.Source, model.Platform, macports.PortInfo, *toleranceCapture) *probeResult {
	return &probeResult{Status: "failed", ErrorCode: "probe-build-required"}
}
