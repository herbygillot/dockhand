package credential

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("credential: not found")

// ErrLocked is a store that keeps the credential but can't give it now,
// as a locked Keychain with nobody at the screen to unlock it. Its reader
// says how to unlock it, or what stands in for it; it's never a reason to
// try another source.
var ErrLocked = errors.New("credential: the store is locked")

type Key struct {
	Service string
	Account string
}

type Store interface {
	Get(context.Context, Key) (string, error)
	Put(context.Context, Key, string) error
}

type DeviceAuthorization struct {
	UserCode        string
	VerificationURL string
	ExpiresAt       time.Time
}

// DeviceFlow logs in with a one-time code, giving a login that renews
// itself.
type DeviceFlow interface {
	Authorize(context.Context, string, func(DeviceAuthorization) error) (Login, error)
}

type Remover interface {
	Delete(context.Context, Key) error
}
