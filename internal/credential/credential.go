package credential

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("credential: not found")

// ErrLocked is a store that keeps the credential but can't give it now,
// as a locked Keychain with nobody at the screen to unlock it. Its reader
// says how to unlock it, or what stands in for it; it's never a reason to
// try another source.
var ErrLocked = errors.New("credential: the store is locked")

// LockedKeychain says the login Keychain locked, with how to unlock it: the
// GitHub login's words, and Tart's, whose VMs need it too (rc7's F5, where
// check-926 built once it was unlocked in the same SSH shell).
const LockedKeychain = "the login Keychain is locked, as it is over SSH with nobody at the screen; unlock it with security unlock-keychain in the same shell, since unlocking it in another SSH session doesn't reach this one"

// LockReporter is a store that can say whether it's locked now, without
// reading a credential: setup's Tart line asks the login Keychain, which
// Tart's VMs need to start too (rc7's F5).
type LockReporter interface {
	Locked(ctx context.Context) bool
}

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
}

// DeviceFlow logs in with a one-time code, giving a login that renews
// itself.
type DeviceFlow interface {
	Authorize(context.Context, string, func(DeviceAuthorization) error) (Login, error)
}

type Remover interface {
	Delete(context.Context, Key) error
}
