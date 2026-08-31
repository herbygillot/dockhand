package intent

import (
	"context"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/distfile"
)

// Fetcher downloads one distfile to dest and reports its checksums; a
// planner asks it once per new distfile. portfetch implements it over
// MacPorts' own curl, which is the engine, singular: an in-process
// alternative existed and was deleted for want of a caller.
//
// The bytes stay at dest rather than being hashed and dropped, so a
// planner that must read inside a distfile — a lockfile for a vendored
// block — reads the same artifact whose checksums it just recorded,
// with no second download to disagree with the first. The caller owns
// dest.
type Fetcher interface {
	Fetch(ctx context.Context, urls []string, opts distfile.Options, dest string) (checksums.Sums, error)
}
