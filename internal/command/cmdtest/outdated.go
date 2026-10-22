package cmdtest

import (
	"context"
	"testing"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
)

// jqIsOutdated stands in for upstream discovery: jq has 1.8.1, and lost
// can't be checked. With uncertain, yq may have 5.0, which was set aside.
type jqIsOutdated struct {
	Asked     []engine.OutdatedRequest
	Uncertain bool
}

func WithOutdated(t *testing.T) *jqIsOutdated {
	reader := &jqIsOutdated{}
	Stands(t).OutdatedReader = reader
	return reader
}

func (j *jqIsOutdated) Outdated(_ context.Context, _ model.ObjectID, request engine.OutdatedRequest) ([]engine.OutdatedPort, error) {
	j.Asked = append(j.Asked, request)
	if request.Progress != nil {
		for done := range 3 {
			request.Progress(done, 2)
		}
	}
	ports := []engine.OutdatedPort{
		{Port: "jq", Current: "1.7.1", Newest: "1.8.1", Outdated: true},
		{Port: "lost", Problem: "no forge could be found for it"},
	}
	if j.Uncertain {
		ports = append(ports, YqMayBeOutdated)
	}
	return ports, nil
}

// YqMayBeOutdated is a port whose newest release is uncertain: v5.0
// compares newer, but was tagged on a commit older than v4.44.1's.
var YqMayBeOutdated = engine.OutdatedPort{Port: "yq", Current: "4.44.1", Newest: "5.0", Uncertain: []engine.SetAside{{Tag: "v5.0", Version: "5.0", Source: "5.0", Predates: "v4.44.1"}}}
