package refresh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/port/porttest"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/plan"
)

// The live evaluator, the fetcher and the handle are porttest's, so
// every package that wants one skips on the same condition.
func newEvaluator(t *testing.T) *eval.Evaluator { return porttest.Evaluator(t) }

func handle(portdir string, ev *eval.Evaluator) port.Handle {
	return porttest.Handle(ev, portdir)
}

func newFetcher(t *testing.T) *portfetch.Fetcher { return porttest.Fetcher(t) }

// distServer serves fixed bytes: a refresh happens at ONE version, so
// unlike a bump's fixture there is nothing path-dependent to model.
// What varies between tests is whether the recorded sums match these
// bytes, not what the bytes are.
func distServer(t *testing.T) (*httptest.Server, []byte) {
	t.Helper()
	content := []byte("what upstream serves today\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)
	return srv, content
}

// refreshPort writes a portdir whose recorded sha256 is the given one —
// the truth, or a stale value, per test.
func refreshPort(t *testing.T, siteURL, versionLine string, sha string, size int) string {
	t.Helper()
	dir := t.TempDir()
	portfile := fmt.Sprintf(`PortSystem 1.0
name refreshee
%s
revision 3
categories devel
maintainers nomaintainer
license MIT
description synthetic refresh target
long_description synthetic refresh target for dockhand tests
master_sites %s
checksums rmd160 0000000000000000000000000000000000000000 \
          sha256 %s \
          size %d
`, versionLine, siteURL, sha, size)
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(portfile), 0o644))
	return dir
}

func shaOf(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestRefreshRepairsStaleChecksums(t *testing.T) {
	ev := newEvaluator(t)
	srv, content := distServer(t)
	// Recorded sha256 is stale; size is right; rmd160 is a placeholder.
	dir := refreshPort(t, srv.URL+"/dist", "version 1.0",
		"1111111111111111111111111111111111111111111111111111111111111111", len(content))

	p, err := Refresh{}.Plan(context.Background(), handle(dir, ev), newFetcher(t))
	require.NoError(t, err)
	require.Len(t, p.Edits, 2, "sha256 and rmd160 move; the size already matches and is not an edit")
	for _, e := range p.Edits {
		assert.NotEqual(t, "version", e.Reason, "a refresh has no version edit")
		assert.NotEqual(t, "revision reset", e.Reason, "nor a revision edit")
	}
	assert.Equal(t, "refresh-checksums", p.Intent)
}

func TestRefreshDeclinesWhenAlreadyTrue(t *testing.T) {
	ev := newEvaluator(t)
	srv, content := distServer(t)
	dir := refreshPort(t, srv.URL+"/dist", "version 1.0", shaOf(content), len(content))
	// The placeholder rmd160 is still wrong, so use a port whose rmd160
	// is also right — compute it the way the fetcher will.
	f := newFetcher(t)
	// First plan repairs the rmd160; apply its value into the fixture,
	// then a second plan must find nothing left to do.
	p, err := Refresh{}.Plan(context.Background(), handle(dir, ev), f)
	require.NoError(t, err)
	require.Len(t, p.Edits, 1, "only the placeholder rmd160")
	src, err := os.ReadFile(filepath.Join(dir, macports.PortfileName))
	require.NoError(t, err)
	fixed := []byte(string(src[:p.Edits[0].Start]) + p.Edits[0].New + string(src[p.Edits[0].End:]))
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), fixed, 0o644))

	_, err = Refresh{}.Plan(context.Background(), handle(dir, ev), f)
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.AlreadyCurrent, d.Type)
}

// The defining difference from bump: no version edit means no version
// location, so a computed version — which bump declines as NotLiteral —
// refreshes without complaint.
func TestRefreshWorksOnAComputedVersion(t *testing.T) {
	ev := newEvaluator(t)
	srv, content := distServer(t)
	dir := refreshPort(t, srv.URL+"/dist", "set v 1.0\nversion ${v}",
		"2222222222222222222222222222222222222222222222222222222222222222", len(content))

	p, err := Refresh{}.Plan(context.Background(), handle(dir, ev), newFetcher(t))
	require.NoError(t, err, "a computed version is no obstacle to a refresh")
	assert.NotEmpty(t, p.Edits)
}

func TestRefreshDeclinesWithoutChecksums(t *testing.T) {
	ev := newEvaluator(t)
	dir := t.TempDir()
	portfile := `PortSystem 1.0
name bare
version 1.0
categories devel
maintainers nomaintainer
license MIT
description no checksums recorded
long_description no checksums recorded at all
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(portfile), 0o644))
	_, err := Refresh{}.Plan(context.Background(), handle(dir, ev), newFetcher(t))
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.ChecksumsNotLocated, d.Type)
}

// A version that moves under a "refresh" is some other change wearing
// this one's name, whatever moved it. The rule is refreshMayChange
// listing checksums and nothing else, and Finish is what asks it — so
// it is tested here against the set this package declares. accept is
// satisfied by the same delta, because the checksums did move: the two
// questions are separate, and both must be asked.
func TestOnlyChecksumsMayMove(t *testing.T) {
	key := info.SubportKey{Subport: "foo"}
	predicted := info.Delta{Changed: map[info.SubportKey][]info.FieldChange{
		key: {
			{Field: info.FieldChecksums, Old: []string{"a"}, New: []string{"b"}},
			{Field: info.FieldVersion, Old: []string{"1.0"}, New: []string{"2.0"}},
		},
	}}
	require.NoError(t, accept(info.Values{Name: "foo", Version: "1.0"}, predicted))

	err := intent.OnlyFields(predicted, refreshMayChange)
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.UnexpectedChange, d.Type)
	assert.Equal(t, "foo: version", d.Detail)
}

func TestAcceptRequiresChecksumsToMove(t *testing.T) {
	vals := info.Values{Name: "foo", Version: "1.0"}
	err := accept(vals, info.Delta{})
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.FetchNotDriven, d.Type)
}
