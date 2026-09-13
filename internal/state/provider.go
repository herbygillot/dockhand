package state

import (
	"context"

	"github.com/herbygillot/dockhand/v2/internal/record"
)

// ProviderStore supplies pool-scoped transactions for provider coordination.
// It is implemented by the same backend as Store, not an independent lock service.
type ProviderStore interface {
	ProviderPool(context.Context, string) (record.ProviderPool, error)
	RegisterProviderPool(context.Context, record.ProviderPool) (record.ProviderPool, error)
	ProviderView(context.Context, string, func(context.Context, ProviderReader) error) error
	ProviderUpdate(context.Context, string, func(context.Context, ProviderTx) error) error
}
type ProviderReader interface {
	Execution(context.Context, record.RequestID) (record.ProviderExecution, error)
	Occupied(context.Context) ([]record.ProviderExecution, error)
}
type ProviderTx interface {
	ProviderReader
	PutExecution(context.Context, record.ProviderExecution) error
}
