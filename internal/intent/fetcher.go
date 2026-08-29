package intent

import (
	"context"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/distfile"
)

// Fetcher supplies distfile checksums; a planner asks it once per new
// distfile. portfetch implements it over MacPorts' own curl — the
// planners' normal engine — and distfile.Direct in-process, for
// contexts with no installation in play.
type Fetcher interface {
	Fetch(ctx context.Context, urls []string, opts distfile.Options) (checksums.Sums, error)
}
