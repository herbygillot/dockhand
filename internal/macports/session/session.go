// Package session is the one port-tclsh bootstrap under eval and
// portfetch: the root guard, the installation's version probe, shim
// selection over one shim set, the shell start, and the rpc handshake
// that loads the shim. Everything that talks to MacPorts' internals
// starts here and differs only in which ops it calls afterwards.
//
// One shim set serves both halves — the evaluator's snapshot, fetchinfo,
// and options next to the fetcher's fetchdist, livecheckrun, and vercmp —
// so mportinit runs once per session and a version mismatch is judged
// in one place. The consumers stay separate sessions where separation
// matters (a stalled download is cancelled by breaking its own session,
// and the evaluator never notices); what they share is how a session
// comes up, not the session itself.
package session

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/shim"
	"github.com/herbygillot/dockhand/internal/tcl/rpc"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
)

//go:embed shims
var shimFS embed.FS

// shimDir is the embedded shim set; see internal/macports/shim for how
// one is chosen.
const shimDir = "shims"

// NewestShim is the highest MacPorts version dockhand has a shim for,
// and so the newest it has been verified to speak to. An installation
// beyond it is driven by an older shim, which works until it does not.
func NewestShim() (string, error) { return shim.Newest(shimFS, shimDir) }

// ErrRootRefused reports that Start declined to run as the superuser
// without AllowRoot.
var ErrRootRefused = errors.New("session: refusing to run as root: a port-tclsh session never requires privileges (pass AllowRoot to override)")

// ErrStartup reports that the session could not be established — the
// tclsh never answered, or the shim failed to initialize. It is the
// domain-level fact callers classify on (a machine problem, not a port
// problem); the transport's own error stays wrapped inside for detail.
var ErrStartup = errors.New("session: port-tclsh session failed to start")

// Session is a port-tclsh conversation with the MacPorts shim loaded.
// It is not safe for concurrent use; parallelism arrives as several
// sessions, not a shared one.
type Session struct {
	sess          *rpc.Session
	shim, version string
}

type config struct {
	allowRoot bool
	// version selects the shim; empty means undetermined, which takes
	// the newest shim available. versionSet records that a caller
	// stated it — an empty stated version is still a statement, and
	// must not trigger a probe the caller already made.
	version    string
	versionSet bool
	inits      []string
}

// Option configures Start.
type Option func(*config)

// AllowRoot permits running the session as the superuser. By default
// Start refuses: evaluation and fetching are reads and never require
// privileges, and mportinit carries writes that are dormant only for an
// unprivileged user (a Spotlight hidden-flag update, registry schema
// work on a mismatched install). Privileged phases — installing,
// building — construct their sessions deliberately and say so with this
// option.
func AllowRoot() Option { return func(c *config) { c.allowRoot = true } }

// WithVersion states which MacPorts the installation runs, so the
// matching shim is loaded without Start probing for it. Callers that
// probed once for many sessions — a pool does — pass what they found,
// an empty string included: undetermined is an answer, and it takes
// the newest shim rather than a second probe.
func WithVersion(v string) Option {
	return func(c *config) {
		c.version = v
		c.versionSet = true
	}
}

// WithInit evaluates further Tcl scripts after the shim, before the
// dispatch loop starts. The order is the point: a platform frame's
// macports::override_vars must run after mportinit, which the shim
// carries.
func WithInit(scripts ...string) Option {
	return func(c *config) { c.inits = append(c.inits, scripts...) }
}

// rootGuard is Start's privilege check, separated for testability.
func rootGuard(euid int, allowRoot bool) error {
	if euid == 0 && !allowRoot {
		return ErrRootRefused
	}
	return nil
}

// Start brings up a session over the installation's port-tclsh. It owns
// the process on every path: a failure after the shell starts kills it,
// so callers never inherit a half-initialized shell. Initialization runs
// mportinit against the installation, so it needs one, and takes on the
// order of a second.
//
// A shim mismatch degrades rather than blocks: an installation that will
// not say its version still gets a session, on the newest shim.
func Start(ctx context.Context, pfx prefix.Prefix, opts ...Option) (*Session, error) {
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	if err := rootGuard(os.Geteuid(), cfg.allowRoot); err != nil {
		return nil, err
	}
	version := cfg.version
	if !cfg.versionSet {
		var err error
		version, err = pfx.Version(ctx)
		if err != nil {
			slog.Debug("macports version undetermined", "prefix", string(pfx), "err", err)
		}
	}
	script, named, err := shim.Select(shimFS, shimDir, version)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStartup, err)
	}
	slog.Debug("session shim", "shim", named, "macports", version)
	proc, err := shell.Start(ctx, pfx.PortTclsh())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStartup, err)
	}
	// rpc.New kills the proc itself on every failure past its claim, so
	// nothing is left running on this path either.
	sess, err := rpc.New(ctx, proc, rpc.WithInit(append([]string{script}, cfg.inits...)...))
	if err != nil {
		return nil, fmt.Errorf("%w: initializing shim: %w", ErrStartup, err)
	}
	return &Session{sess: sess, shim: named, version: version}, nil
}

// Call invokes the named shim op with arguments and returns the reply.
func (s *Session) Call(ctx context.Context, op string, args ...string) (string, error) {
	return s.sess.Call(ctx, op, args...)
}

// Close shuts the session and its process down.
func (s *Session) Close() error { return s.sess.Close() }

// Shim is the MacPorts version the loaded shim was written for.
func (s *Session) Shim() string { return s.shim }

// Version is the MacPorts version the installation reported, or was
// stated to run; empty when undetermined.
func (s *Session) Version() string { return s.version }
