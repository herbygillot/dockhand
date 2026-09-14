package verify

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/record"
)

var ErrLogUnavailable = errors.New("verification log unavailable (it may have been pruned)")

// ArtifactPruner removes diagnostic files only after resource release. Calls
// must be idempotent and serialized against provider operations and log reads.
// Durable execution identities and results must survive pruning.
type ArtifactPruner interface {
	PruneArtifacts(context.Context, record.ResourceHandle) error
}
