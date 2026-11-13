//go:build surveymeasure

package main

import (
	"context"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
)

func measurePolicies(ctx context.Context, p macports.PortObservation, data []byte, root string) (observe.Explanation, observe.Explanation, error) {
	return observe.CompareTolerance(ctx, p, data, root)
}
func measurementAvailable() bool { return true }
