package credential

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("credential: not found")

type Key struct {
	Service string
	Account string
}

type Value struct {
	Secret  string
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

type DeviceFlow interface {
	Authorize(context.Context, string, func(DeviceAuthorization) error) (Value, error)
}

type Remover interface {
	Delete(context.Context, Key) error
}
