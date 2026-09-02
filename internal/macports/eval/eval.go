package eval

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/session"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// Evaluator owns a port-tclsh session with the MacPorts shim loaded.
// It is not safe for concurrent use; parallelism arrives as a pool of
// evaluators, not a shared one.
type Evaluator struct {
	sess *session.Session
}

type config struct {
	platform info.Platform
	// sopts are the session options the evaluator's own options
	// translate to; the platform frame joins them at Start, because it
	// is an init script and must follow the shim.
	sopts []session.Option
}

// Option configures Start.
type Option func(*config)

// AllowRoot permits running the evaluator as the superuser. By default
// Start refuses: evaluation is a pure read and never requires
// privileges, and mportinit carries writes that are dormant only for an
// unprivileged user (a Spotlight hidden-flag update, registry schema
// work on a mismatched install). Privileged phases — installing,
// building — construct their sessions deliberately and say so with this
// option.
func AllowRoot() Option {
	return func(c *config) { c.sopts = append(c.sopts, session.AllowRoot()) }
}

// ErrRootRefused reports that Start declined to run as the superuser
// without AllowRoot. It is session's sentinel, kept under this name
// because the exit table and callers classify on it.
var ErrRootRefused = session.ErrRootRefused

// ErrStartup reports that the evaluator's session could not be
// established — the tclsh never answered, or the shim failed to
// initialize. It is the domain-level fact callers classify on (a
// machine problem, not a port problem); the transport's own error stays
// wrapped inside for detail. It is session's sentinel, kept under this
// name because the exit table and callers classify on it, so every
// session start failure satisfies errors.Is against it.
var ErrStartup = session.ErrStartup

// WithMacPortsVersion states which MacPorts the installation runs, so
// the matching shim is loaded without Start probing for it. Callers
// that probed once for many evaluators — a pool does — pass what they
// found; without it the session probes, and an undetermined version
// takes the newest shim.
func WithMacPortsVersion(v string) Option {
	return func(c *config) { c.sopts = append(c.sopts, session.WithVersion(v)) }
}

// WithPlatform evaluates every Portfile as though on the given platform,
// via the same macports::override_vars mechanism base's portindex -p uses.
// The frame is per-evaluator and permanent: override_vars removes variable
// traces, so a spoofed session cannot be returned to host truth — an
// evaluator is created for a frame and never reused outside it. Simulation
// is evaluation-level: PortGroups that probe the host filesystem or exec
// host tools will still see the host.
func WithPlatform(p info.Platform) Option {
	return func(c *config) { c.platform = p }
}

// Start builds an evaluator over a fresh session against an
// installation. The session owns the process on every path — a failure
// after the shell starts kills it, so callers never inherit a
// half-initialized shell — and its initialization runs mportinit
// against the installation, so it needs one, and takes on the order of
// a second.
func Start(ctx context.Context, pfx prefix.Prefix, opts ...Option) (*Evaluator, error) {
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	sopts := cfg.sopts
	if !cfg.platform.IsZero() {
		sopts = append(sopts, session.WithInit(platformOverrides(cfg.platform)))
	}
	s, err := session.Start(ctx, pfx, sopts...)
	if err != nil {
		return nil, err
	}
	slog.Debug("evaluator shim", "shim", s.Shim(), "macports", s.Version())
	return &Evaluator{sess: s}, nil
}

// Close shuts the evaluator down.
func (e *Evaluator) Close() error { return e.sess.Close() }

// Snapshot evaluates every context the Portfile defines under default
// variants: the top-level port, then one evaluation per subport. Any
// context failing to evaluate fails the whole snapshot — a partial
// snapshot would silently weaken every check built on totality (D13).
func (e *Evaluator) Snapshot(ctx context.Context, portdir string, variants info.VariantSet) (info.Snapshot, error) {
	top, subs, err := e.one(ctx, portdir, "", variants)
	if err != nil {
		return nil, err
	}
	snap := info.Snapshot{info.SubportKey{Subport: top.Name, Variants: variants}: top}
	for _, sp := range subs {
		v, _, err := e.one(ctx, portdir, sp, variants)
		if err != nil {
			return nil, fmt.Errorf("eval: subport %s: %w", sp, err)
		}
		// Keyed by the requested context, which is the identity being
		// evaluated regardless of what the values claim to be named.
		snap[info.SubportKey{Subport: sp, Variants: variants}] = v
	}
	return snap, nil
}

// Values evaluates one context — the top-level port when subport is
// empty — and returns its state. One evaluation, whichever context is
// named: asking for a subport costs the same as asking for the top
// level, and never requires enumerating the rest.
//
// A subport the Portfile does not define is an error from MacPorts
// itself ("X does not have a subport Y"), so the name is validated by
// evaluation rather than by a prior enumeration.
func (e *Evaluator) Values(ctx context.Context, portdir, subport string, variants info.VariantSet) (info.Values, error) {
	v, _, err := e.one(ctx, portdir, subport, variants)
	return v, err
}

// Top evaluates only the top-level context. This is the census path —
// classification of a port's style needs the top context's values and
// nothing else.
func (e *Evaluator) Top(ctx context.Context, portdir string, variants info.VariantSet) (info.Values, error) {
	return e.Values(ctx, portdir, "", variants)
}

// Subports enumerates the port's subports with a single evaluation, without
// building the full snapshot.
func (e *Evaluator) Subports(ctx context.Context, portdir string) ([]string, error) {
	_, subs, err := e.one(ctx, portdir, "", "")
	return subs, err
}

// one evaluates a single context: the top level when subport is empty.
func (e *Evaluator) one(ctx context.Context, portdir, subport string, variants info.VariantSet) (info.Values, []string, error) {
	args := []string{portdir, subport, variationsArg(variants)}
	reply, err := e.sess.Call(ctx, "snapshot", args...)
	if err != nil {
		return info.Values{}, nil, fmt.Errorf("eval: snapshot of %s: %w", portdir, err)
	}
	v, subs, err := decodeSnapshot(reply)
	if err != nil {
		return info.Values{}, nil, fmt.Errorf("eval: snapshot of %s: %w", portdir, err)
	}
	return v, subs, nil
}

// variationsArg renders a variant frame as mportopen's variations list:
// alternating variant names and +/- signs. Variant names are restricted to
// list-safe characters, so plain joining is quoting-correct.
func variationsArg(v info.VariantSet) string {
	var b []byte
	for _, sel := range v.List() {
		if len(b) > 0 {
			b = append(b, ' ')
		}
		b = append(b, sel[1:]...)
		b = append(b, ' ', sel[0])
	}
	return string(b)
}

// FetchInfo is one evaluation context's fetch surface: each distfile
// with the full URLs it may be fetched from, plus the port's own fetch
// exceptions — the fetch.* options portfetch itself threads through to
// curl. Ports that fetch from a repository rather than distfiles have
// no Files.
type FetchInfo struct {
	Files         map[string][]string
	DisableEPSV   bool
	IgnoreSSLCert bool
	UserAgent     string
}

// FetchInfo reports the fetch surface for one evaluation context —
// URLs assembled by MacPorts' own portfetch machinery, mirror macros
// expanded. noMirrors skips the MacPorts fallback mirrors (the switch
// behind port fetch --no-mirrors): the right mode when the distfiles
// sought are for a version the mirrors cannot have yet.
func (e *Evaluator) FetchInfo(ctx context.Context, portdir, subport string, variants info.VariantSet, noMirrors bool) (FetchInfo, error) {
	nm := "0"
	if noMirrors {
		nm = "1"
	}
	reply, err := e.sess.Call(ctx, "fetchinfo", portdir, subport, variationsArg(variants), nm)
	if err != nil {
		return FetchInfo{}, fmt.Errorf("eval: fetchinfo of %s: %w", portdir, err)
	}
	fields, errs := syntax.DictValues(reply)
	if len(errs) != 0 {
		return FetchInfo{}, fmt.Errorf("eval: fetchinfo of %s: malformed reply %q: %w", portdir, reply, errs[0])
	}
	fileFields, errs := syntax.DictValues(fields["files"])
	if len(errs) != 0 {
		return FetchInfo{}, fmt.Errorf("eval: fetchinfo of %s: malformed files dict %q: %w", portdir, fields["files"], errs[0])
	}
	epsv, haveEpsv := fields["use_epsv"]
	sslcert, haveSslcert := fields["ignore_sslcert"]
	fi := FetchInfo{
		Files: make(map[string][]string, len(fileFields)),
		// portfetch's own tests, with its defaults when the reply lacks
		// the option: epsv on, certificates verified.
		DisableEPSV:   haveEpsv && epsv != "yes",
		IgnoreSSLCert: haveSslcert && sslcert != "no",
		UserAgent:     syntax.ListValue(fields["user_agent"]),
	}
	for file, raw := range fileFields {
		urls, errs := syntax.ListValues(raw)
		if len(errs) != 0 {
			return FetchInfo{}, fmt.Errorf("eval: fetchinfo of %s: malformed url list %q: %w", portdir, raw, errs[0])
		}
		fi.Files[file] = urls
	}
	return fi, nil
}

// Options reads the named port options for one evaluation context,
// omitting options the port does not have. Values are decoded as single
// list elements — the right reading for scalar options (URLs, regexes,
// names); list-valued options need their own decoding and their own
// accessor.
func (e *Evaluator) Options(ctx context.Context, portdir, subport string, variants info.VariantSet, names ...string) (map[string]string, error) {
	args := append([]string{portdir, subport, variationsArg(variants)}, names...)
	reply, err := e.sess.Call(ctx, "options", args...)
	if err != nil {
		return nil, fmt.Errorf("eval: options of %s: %w", portdir, err)
	}
	fields, errs := syntax.DictValues(reply)
	if len(errs) != 0 {
		return nil, fmt.Errorf("eval: options of %s: malformed reply %q: %w", portdir, reply, errs[0])
	}
	out := make(map[string]string, len(fields))
	for name, raw := range fields {
		out[name] = syntax.ListValue(raw)
	}
	return out, nil
}
