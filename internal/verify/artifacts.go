package verify

import (
	"context"
	"errors"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
)

var ErrLogUnavailable = errors.New("verification log unavailable (it may have been pruned)")

// ArtifactPruner removes diagnostic files only after resource release. Calls
// must be idempotent and serialized against provider operations and log reads.
// Durable execution identities and results must survive pruning.
type ArtifactPruner interface {
	PruneArtifacts(context.Context, record.ResourceHandle) error
}

// ErrCacheBusy reports a log cache a download holds the lock on: nothing was
// pruned, and the next sweep may find it free. It is distinct from a cache
// that was never written, which prunes nothing and reports no error.
var ErrCacheBusy = errors.New("verify: log cache is in use")

// LogCachePruner removes only local, re-downloadable diagnostics for an eligible
// terminal attempt selected by workflow. It never removes evidence or remote logs.
// The bool reports eligible local files; a busy cache reports ErrCacheBusy. Dry
// runs must not initialize storage.
type LogCachePruner interface {
	PruneLogCache(context.Context, record.ProviderRun, time.Time, bool) (bool, error)
}
