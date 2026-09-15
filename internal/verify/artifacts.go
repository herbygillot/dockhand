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

// LogCachePruner removes only local, re-downloadable diagnostics for an eligible
// terminal attempt selected by workflow. It never removes evidence or remote logs.
// The bool reports eligible local files. Dry runs must not initialize storage.
type LogCachePruner interface {
	PruneLogCache(context.Context, record.ProviderRun, time.Time, bool) (bool, error)
}
