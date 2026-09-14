package state

import (
	"context"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
)

// ProviderStore supplies provider coordination and shared image observations.
// It is implemented by the same backend as Store, not an independent lock service.
type ProviderStore interface {
	ImageCache
	ImageCapabilityCache
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

// ImageDigest is a disposable observation of an image's content at one file stamp.
// Provider and Path identify the image across repository entries in the same DB.
type ImageDigest struct {
	Provider, Path, Stamp, Digest string
}

type ImageCache interface {
	ImageDigest(context.Context, string, string) (ImageDigest, error)
	PutImageDigest(context.Context, ImageDigest) error
}

type ImageCapabilityCache interface {
	ImageCapabilities(context.Context, string, string) (ImageCapabilities, error)
	PutImageCapabilities(context.Context, ImageCapabilities) error
}

// ImageCapabilities is a disposable observation shared by repositories using
// the same provider and immutable environment content.
type ImageCapabilities struct {
	Provider          string
	EnvironmentDigest string
	CapabilityDigest  string
	Capabilities      record.EnvironmentCapabilities
	Problem           string
	ObservedAt        time.Time
}
