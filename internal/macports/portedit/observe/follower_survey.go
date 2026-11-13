//go:build surveymeasure

package observe

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports"
	"reflect"
)

//go:embed follow.go
var surveyFollowerSource string

const surveyFollowerSHA = "531b02d76bfb86573bcf742ff15dcae1f73751671f14fe8a7fb56c160c3bc667"

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
	if fmt.Sprintf("%x", sha256.Sum256([]byte(surveyFollowerSource))) != surveyFollowerSHA {
		return Explanation{}, Explanation{}, fmt.Errorf("survey parity requires follow.go unchanged from a0298da0")
	}
	before, bad := legacyTolerate(ctx, port, contents, root)
	on := Explain(ctx, port, contents, root)
	if bad != on.Inconclusive || !reflect.DeepEqual(before, on.Observation) {
		return on, Explanation{}, fmt.Errorf("tolerance parity failure against a0298da0")
	}
	off := Explain(context.WithValue(ctx, withheldFollower{}, true), port, contents, root)
	return on, off, nil
}
