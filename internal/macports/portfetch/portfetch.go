// Package portfetch fetches distfiles through MacPorts' own machinery:
// a dedicated port-tclsh session running base's pextlib curl — the same
// code, learned exceptions, and macports.conf configuration (proxies
// included) that port itself fetches with. The session outlives one
// fetch: a Fetcher is created per plan and serves every download the
// plan needs.
//
// The session is deliberately separate from the evaluator's: a stalled
// download is cancelled by breaking this session, and the evaluator
// never notices. Files land in the Fetcher's own temporary directory —
// never the shared distpath — so concurrent port-tclsh processes share
// no writable state.
package portfetch

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/distfile"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/tcl/rpc"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
)

//go:embed fetch.tcl
var fetchScript string

// ErrRootRefused mirrors eval's guard: fetching never requires
// privileges, and mportinit carries writes that are dormant only for an
// unprivileged user.
var ErrRootRefused = errors.New("portfetch: refusing to run as root: fetching never requires privileges")

// Fetcher is a fetch session over one installation's port-tclsh. Not
// safe for concurrent use; downloads through one Fetcher are serial.
type Fetcher struct {
	sess   *rpc.Session
	tmpDir string
	n      int
}

// New starts the fetch session against an installation.
func New(ctx context.Context, pfx prefix.Prefix) (*Fetcher, error) {
	if os.Geteuid() == 0 {
		return nil, ErrRootRefused
	}
	proc, err := shell.Start(ctx, pfx.PortTclsh())
	if err != nil {
		return nil, err
	}
	sess, err := rpc.New(ctx, proc, rpc.WithInit(fetchScript))
	if err != nil {
		return nil, fmt.Errorf("portfetch: initializing fetch session: %w", err)
	}
	tmpDir, err := os.MkdirTemp("", "dockhand-fetch-*")
	if err != nil {
		sess.Close() //nolint:errcheck // best-effort on the error path
		return nil, err
	}
	return &Fetcher{sess: sess, tmpDir: tmpDir}, nil
}

// Close shuts the session down and removes any fetched remains.
func (f *Fetcher) Close() {
	f.sess.Close()         //nolint:errcheck // best-effort shutdown
	os.RemoveAll(f.tmpDir) //nolint:errcheck // temp dir cleanup
}

// Fetch downloads one distfile, trying urls in order — the order
// MacPorts' own machinery proposed — and returns its checksums. The
// fetched bytes are hashed from disk and discarded.
func (f *Fetcher) Fetch(ctx context.Context, urls []string, opts distfile.Options) (checksums.Sums, error) {
	f.n++
	dest := filepath.Join(f.tmpDir, fmt.Sprintf("distfile-%d", f.n))
	defer os.Remove(dest) //nolint:errcheck // temp file cleanup

	var lastErr error
	for _, url := range urls {
		_, err := f.sess.Call(ctx, "fetchdist", url, dest,
			flag(opts.DisableEPSV), flag(opts.IgnoreSSLCert), opts.UserAgent)
		if err != nil {
			slog.Debug("fetch failed", "url", url, "err", err)
			lastErr = fmt.Errorf("%s: %w", url, err)
			if ctx.Err() != nil {
				break
			}
			continue
		}
		slog.Debug("fetched", "url", url)
		return checksums.HashFile(dest)
	}
	return checksums.Sums{}, fmt.Errorf("%w (%d urls): %w", distfile.ErrUnavailable, len(urls), lastErr)
}

func flag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// LivecheckResult is what the port's livecheck phase reported.
type LivecheckResult struct {
	Ran      bool   // false: livecheck is type none, an absent witness
	Version  string // the newer version livecheck found, when it found one
	UpToDate bool   // livecheck ran and matched the current version
	NoMatch  bool   // the regex matched nothing: the rot signal
}

// Livecheck executes the port's own livecheck phase — mportexec of the
// livecheck target, exactly what `port livecheck` drives — so every
// livecheck.type works, "default" resolution and the tree's type files
// included. The phase's ui output is captured through MacPorts'
// embedder API and parsed into the result; an error means the check
// could not run (fetch failed, type unknown), never that the port is
// up to date.
func (f *Fetcher) Livecheck(ctx context.Context, portdir, subport string) (LivecheckResult, error) {
	f.n++
	dest := filepath.Join(f.tmpDir, fmt.Sprintf("livecheck-%d.log", f.n))
	defer os.Remove(dest) //nolint:errcheck // temp file cleanup
	out, err := f.sess.Call(ctx, "livecheckrun", portdir, subport, dest)
	if err != nil {
		return LivecheckResult{}, fmt.Errorf("portfetch: livecheck of %s: %w", portdir, err)
	}
	return parseLivecheck(out)
}

var (
	lcUpdated = regexp.MustCompile(`seems to have been updated \(port version: .*, new version: (.+)\)$`)
	lcOlder   = regexp.MustCompile(`extracted version '([^']+)' is older`)
)

// parseLivecheck reads the phase's priority-tagged ui lines.
func parseLivecheck(out string) (LivecheckResult, error) {
	var r LivecheckResult
	for line := range strings.Lines(out) {
		priority, msg, ok := strings.Cut(strings.TrimRight(line, "\n"), "\t")
		if !ok {
			continue
		}
		switch {
		case priority == "msg":
			if m := lcUpdated.FindStringSubmatch(msg); m != nil {
				r.Ran, r.Version = true, m[1]
			}
		case priority == "info" && strings.Contains(msg, "seems to be up to date"):
			r.Ran, r.UpToDate = true, true
		case priority == "error" && strings.Contains(msg, "regex didn't match"):
			r.Ran, r.NoMatch = true, true
		case priority == "error":
			if m := lcOlder.FindStringSubmatch(msg); m != nil {
				// Livecheck extracted a version older than the port's:
				// real testimony, oddly shaped; report it as found.
				r.Ran, r.Version = true, m[1]
				continue
			}
			// Any other error: the check could not run.
			return LivecheckResult{}, fmt.Errorf("portfetch: livecheck: %s", msg)
		}
	}
	return r, nil
}

// Vercmp compares two versions under MacPorts' own ordering: negative
// when a < b, zero when equal, positive when a > b. Version comparison
// is base's semantics or it is wrong.
func (f *Fetcher) Vercmp(ctx context.Context, a, b string) (int, error) {
	reply, err := f.sess.Call(ctx, "vercmp", a, b)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(reply)
	if err != nil {
		return 0, fmt.Errorf("portfetch: vercmp said %q: %w", reply, err)
	}
	return n, nil
}
