package bump

import (
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

	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/macports/tree"
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

// distServer serves the same fixed bytes for every requested distfile.
func distServer(t *testing.T) (*httptest.Server, []byte) {
	t.Helper()
	content := []byte("synthetic tarball bytes\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)
	return srv, content
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
	srv, content := distServer(t)
	dir := bumpPort(t, srv.URL+"/dist", content)

	b := Bump{Version: "2.0"}
	p, err := b.Plan(context.Background(), handle(dir, ev), newFetcher(t))
	require.NoError(t, err)

	// version + revision reset + rmd160 + sha256 (size is unchanged:
	// same served bytes, so no size edit is needed — the recorded size
	// still matches and its edit is a no-op... the planner still emits
	// it; assert by reason instead).
	reasons := make(map[string]int)
	for _, e := range p.Edits {
		reasons[e.Reason]++
	}
	assert.Equal(t, 1, reasons["version"])
	assert.Equal(t, 1, reasons["revision reset"])
	assert.Equal(t, 1, reasons["checksum rmd160"])
	assert.Equal(t, 1, reasons["checksum sha256"])
	assert.Equal(t, 1, reasons["checksum size"])

	sha := sha256.Sum256(content)
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
	srv, content := distServer(t)
	dir := bumpPort(t, srv.URL+"/dist", content)
	_, err := Bump{Version: "1.0"}.Plan(context.Background(), handle(dir, ev), newFetcher(t))
	var d *intent.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, intent.AlreadyCurrent, d.Type)
}

func TestBumpDeclinesFetchNotDriven(t *testing.T) {
	// A pinned-distname port: the version edit moves nothing derived,
	// which is the straddle signature.
	ev := newEvaluator(t)
	srv, content := distServer(t)
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
	var d *intent.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, intent.FetchNotDriven, d.Type)
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
	f, err := portfetch.New(context.Background(), prefix.Prefix(filepath.Dir(filepath.Dir(tclsh))))
	require.NoError(t, err)
	t.Cleanup(f.Close)
	return f
}
