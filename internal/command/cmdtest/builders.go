package cmdtest

import (
	"context"

	"github.com/herbygillot/dockhand/internal/macports/buildhistory"
)

// BuildHistories stand in for ports.macports.org's build history, by
// port: a port it holds has that history, and one it doesn't, no answer,
// so no test asks the network. It's an engine's BuilderHistory.
type BuildHistories map[string]buildhistory.History

func (h BuildHistories) History(_ context.Context, port string) (buildhistory.History, bool) {
	history, ok := h[port]
	return history, ok
}
