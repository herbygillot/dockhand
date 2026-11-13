//go:build surveymeasure

package observe

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"reflect"
)

type withheldFollower struct{}

func followValue(ctx context.Context, sources [][]byte, option string, base *baseCode) ([]string, bool) {
	if ctx != nil && ctx.Value(withheldFollower{}) == true {
		return nil, false
	}
	return followedReadsBenign(sources, option, base)
}

// CompareTolerance exists only in surveymeasure builds. It refuses a
// changed normal verdict before returning a pair; the upcoming corpus run
// therefore checks parity on every complete observation it measures.
func CompareTolerance(ctx context.Context, port macports.PortObservation, contents []byte, root string) (Explanation, Explanation, error) {
	before, bad := legacyTolerate(ctx, port, contents, root)
	on := Explain(ctx, port, contents, root)
	if bad != on.Inconclusive || !reflect.DeepEqual(before, on.Observation) {
		return on, Explanation{}, fmt.Errorf("tolerance parity failure against a0298da0")
	}
	off := Explain(context.WithValue(ctx, withheldFollower{}, true), port, contents, root)
	return on, off, nil
}
