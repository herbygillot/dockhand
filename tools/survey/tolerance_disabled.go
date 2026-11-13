//go:build !surveymeasure

package main

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit/observe"
)

func measurePolicies(context.Context, macports.PortObservation, []byte, string) (observe.Explanation, observe.Explanation, error) {
	return observe.Explanation{}, observe.Explanation{}, fmt.Errorf("follower measurement requires a survey built with -tags surveymeasure")
}
func measurementAvailable() bool { return false }
