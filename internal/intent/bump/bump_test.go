package bump

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tempdir"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/tree"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
	"github.com/herbygillot/dockhand/internal/testenv"
)

func newEvaluator(t *testing.T) *eval.Evaluator {
	t.Helper()
	path := testenv.PortTclsh(t)
	proc, err := shell.Start(context.Background(), path)
	require.NoError(t, err)
	ev, err := eval.New(context.Background(), proc)
	require.NoError(t, err)
	t.Cleanup(func() { ev.Close() })
	return ev
}

// handle binds a portdir to an evaluator, as the command does.
func handle(portdir string, ev *eval.Evaluator) port.Handle {
	return port.New(tree.Target{Portdir: portdir}, ev)
}

// servedFor is the body distServer returns for a path. Two versions are
// two distfiles, so their bytes must differ — a server answering every
// request alike would let a bump pass its checksum assertions without
// the checksums having anywhere to move. The length varies too, derived
// from the path's own digest, because paths differing only in a version
// digit are the same length and would leave the recorded size unmoved.
func servedFor(path string) []byte {
	sum := sha256.Sum256([]byte(path))
	return bytes.Repeat([]byte(path+"\n"), 1+int(sum[0])%8)
}

// distServer serves bytes derived from the path requested.
func distServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(servedFor(r.URL.Path))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// bumpPort builds a portdir whose distfile checksums are correct for
// the served content at version 1.0. The site URL needs a path segment:
// a bare host:port would read as portfetch's site:tag syntax.
func bumpPort(t *testing.T, siteURL string, content []byte) string {
	t.Helper()
	sha := sha256.Sum256(content)
	dir := t.TempDir()
	portfile := fmt.Sprintf(`PortSystem 1.0
name bumpee
version 1.0
revision 2
categories devel
maintainers nomaintainer
license MIT
description synthetic bump target
long_description synthetic bump target for dockhand tests
master_sites %s
checksums rmd160 0000000000000000000000000000000000000000 \
          sha256 %s \
          size %d
`, siteURL, hex.EncodeToString(sha[:]), len(content))
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(portfile), 0o644))
	return dir
}

func TestBumpPlanEndToEnd(t *testing.T) {
	ev := newEvaluator(t)
	srv := distServer(t)
	content := servedFor("/dist/bumpee-1.0.tar.gz")
	dir := bumpPort(t, srv.URL+"/dist", content)

	b := Bump{Version: "2.0"}
	p, err := b.Plan(context.Background(), handle(dir, ev), newFetcher(t))
	require.NoError(t, err)

	// version, revision reset, and every checksum: 2.0 is a different
	// distfile from 1.0, so all three recorded values have somewhere to
	// move. A value that did not move would not be an edit at all.
	reasons := make(map[string]int)
	for _, e := range p.Edits {
		reasons[e.Reason]++
	}
	assert.Equal(t, 1, reasons["version"])
	assert.Equal(t, 1, reasons["revision reset"])
	assert.Equal(t, 1, reasons["checksum rmd160"])
	assert.Equal(t, 1, reasons["checksum sha256"])
	assert.Equal(t, 1, reasons["checksum size"])

	// The new sha256 must be of the NEW distfile. With a server that
	// answered every path alike this assertion could not tell whether
	// the planner had fetched 2.0 at all; now it can.
	sha := sha256.Sum256(servedFor("/dist/bumpee-2.0.tar.gz"))
	var sawSha bool
	for _, e := range p.Edits {
		if e.Reason == "checksum sha256" {
			assert.Equal(t, hex.EncodeToString(sha[:]), e.New)
			sawSha = true
		}
	}
	assert.True(t, sawSha)

	// The prediction covers the version, revision, and checksum moves.
	require.Len(t, p.Predicted, 1)
	fields := make(map[string]bool)
	for _, ch := range p.Predicted[0].Changes {
		fields[ch.Field] = true
	}
	assert.True(t, fields["version"])
	assert.True(t, fields["revision"])
	assert.True(t, fields["checksums"])
	assert.True(t, fields["distfiles"])

	// Apply it: the observed delta must equal the prediction.
	_, err = p.Apply(context.Background(), ev)
	require.NoError(t, err)
	after, err := os.ReadFile(filepath.Join(dir, macports.PortfileName))
	require.NoError(t, err)
	assert.Contains(t, string(after), "version 2.0")
	assert.Contains(t, string(after), "revision 0")
}

func TestBumpDeclinesAlreadyCurrent(t *testing.T) {
	ev := newEvaluator(t)
	srv := distServer(t)
	content := servedFor("/dist/bumpee-1.0.tar.gz")
	dir := bumpPort(t, srv.URL+"/dist", content)
	_, err := Bump{Version: "1.0"}.Plan(context.Background(), handle(dir, ev), newFetcher(t))
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.AlreadyCurrent, d.Type)
}

func TestBumpDeclinesFetchNotDriven(t *testing.T) {
	// A pinned-distname port: the version edit moves nothing derived,
	// which is the straddle signature.
	ev := newEvaluator(t)
	srv := distServer(t)
	content := servedFor("/dist/pinned-fixed.tar.gz")
	sha := sha256.Sum256(content)
	dir := t.TempDir()
	portfile := fmt.Sprintf(`PortSystem 1.0
name pinned
version 1.0
categories devel
maintainers nomaintainer
license MIT
description pinned distname
long_description pinned distname straddle shape
master_sites %s/dist
distname pinned-fixed
checksums rmd160 0000000000000000000000000000000000000000 \
          sha256 %s \
          size %d
`, srv.URL, hex.EncodeToString(sha[:]), len(content))
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(portfile), 0o644))

	_, err := Bump{Version: "2.0"}.Plan(context.Background(), handle(dir, ev), newFetcher(t))
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.FetchNotDriven, d.Type)
}

func TestBumpDeclinesComputedVersion(t *testing.T) {
	ev := newEvaluator(t)
	dir := t.TempDir()
	portfile := `PortSystem 1.0
name computed
set v 1.0
version ${v}
categories devel
maintainers nomaintainer
license MIT
description computed version
long_description computed version declines
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(portfile), 0o644))
	_, err := Bump{Version: "2.0"}.Plan(context.Background(), handle(dir, ev), newFetcher(t))
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "literal")
}

// newFetcher builds the MacPorts-driven fetcher against the discovered
// installation, mirroring what cmd wires in.
func newFetcher(t *testing.T) *portfetch.Fetcher {
	t.Helper()
	tclsh := testenv.PortTclsh(t)
	f, err := portfetch.New(context.Background(), prefix.Prefix(filepath.Dir(filepath.Dir(tclsh))), tempdir.Root{})
	require.NoError(t, err)
	t.Cleanup(f.Close)
	return f
}

// --force plans at the version the port already carries. The version
// itself is not rewritten — that edit would change nothing — and the
// revision is left alone: resetting it where the version did not move
// would send the port backwards in MacPorts' ordering.
func TestBumpForceProceedsAtTheCurrentVersion(t *testing.T) {
	ev := newEvaluator(t)
	srv := distServer(t)
	content := servedFor("/dist/bumpee-1.0.tar.gz")
	dir := bumpPort(t, srv.URL+"/dist", content)

	p, err := Bump{Version: "1.0", Force: true}.Plan(context.Background(), handle(dir, ev), newFetcher(t))
	require.NoError(t, err, "force must survive what declines as AlreadyCurrent")
	for _, e := range p.Edits {
		assert.NotEqual(t, "version", e.Reason, "the version is not rewritten to itself")
		assert.NotEqual(t, "revision reset", e.Reason, "the revision belongs to a version that moved")
	}
}

// The fixture records a placeholder rmd160 against real content, which
// is the shape of a stealth update: bytes that no longer match what the
// Portfile claims about them. A forced run finds exactly that one value
// and leaves the two that are already right alone.
func TestBumpForceRepairsAStaleChecksum(t *testing.T) {
	ev := newEvaluator(t)
	srv := distServer(t)
	content := servedFor("/dist/bumpee-1.0.tar.gz")
	dir := bumpPort(t, srv.URL+"/dist", content)

	p, err := Bump{Version: "1.0", Force: true}.Plan(context.Background(), handle(dir, ev), newFetcher(t))
	require.NoError(t, err)
	require.Len(t, p.Edits, 1, "only the stale value is an edit")
	assert.Equal(t, "checksum rmd160", p.Edits[0].Reason)
	assert.Equal(t, "0000000000000000000000000000000000000000", p.Edits[0].Old)
	assert.NotEqual(t, p.Edits[0].Old, p.Edits[0].New)
}

// accept is pure, so the judgment a forced run is held to needs no
// installation: the version must stay put, where an ordinary bump
// requires it to move.
func TestAcceptForcedRunRefusesAVersionThatMoves(t *testing.T) {
	vals := info.Values{Name: "foo", Version: "1.0"}
	key := info.SubportKey{Subport: "foo"}
	moved := info.Delta{Changed: map[info.SubportKey][]info.FieldChange{
		key: {{Field: info.FieldVersion, Old: []string{"1.0"}, New: []string{"2.0"}}},
	}}
	err := Bump{Version: "1.0", Force: true}.accept(vals, moved)
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.UnexpectedChange, d.Type)
}

// And an ordinary bump still requires the version to arrive.
func TestAcceptOrdinaryBumpRequiresTheVersionToMove(t *testing.T) {
	vals := info.Values{Name: "foo", Version: "1.0"}
	err := Bump{Version: "2.0"}.accept(vals, info.Delta{})
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.TargetNotReached, d.Type)
}
