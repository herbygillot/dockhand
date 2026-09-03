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

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/intent"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/port/porttest"
	"github.com/herbygillot/dockhand/internal/macports/portfetch"
	"github.com/herbygillot/dockhand/internal/plan"
)

// The live evaluator, the fetcher and the handle are porttest's: four
// packages had written them, and a helper that skips in one and requires
// in another is how a broken MacPorts looks green here and red there.
func newEvaluator(t *testing.T) *eval.Evaluator { return porttest.Evaluator(t) }

// handle binds a portdir to an evaluator, as the command does.
func handle(portdir string, ev *eval.Evaluator) port.Handle {
	return porttest.Handle(ev, portdir)
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
	portfile := fmt.Sprintf(`# -*- coding: utf-8; mode: tcl -*-
PortSystem 1.0
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

// A bump asked for the version the port already carries declines — and
// says what it held back on the way past. This fixture already opens
// with a modeline, so there is nothing to hold: the decline stays in the
// ordinary declined band, and the fixture's own header is the reason.
// The withheld case is the one below it.
func TestBumpDeclinesAlreadyCurrent(t *testing.T) {
	ev := newEvaluator(t)
	srv := distServer(t)
	content := servedFor("/dist/bumpee-1.0.tar.gz")
	dir := bumpPort(t, srv.URL+"/dist", content)
	_, err := Bump{Version: "1.0"}.Plan(context.Background(), handle(dir, ev), newFetcher(t))
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.AlreadyCurrent, d.Type)
	assert.Empty(t, d.Withheld)
	assert.Equal(t, exitcode.PlanDeclined, d.DockhandExit())
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

func TestBumpPlansThroughASetVariable(t *testing.T) {
	// Once the classic computed-version decline; the set variable style
	// locates it now, and the bump edits the set's own literal.
	ev := newEvaluator(t)
	dir := t.TempDir()
	portfile := `PortSystem 1.0
name computed
set v 1.0
version ${v}
categories devel
maintainers nomaintainer
license MIT
description version via a set variable
long_description version via a set variable plans
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(portfile), 0o644))
	p, err := Bump{Version: "2.0"}.Plan(context.Background(), handle(dir, ev), newFetcher(t))
	require.NoError(t, err)
	var versionEdit bool
	for _, e := range p.Edits {
		if e.Reason == "version" {
			versionEdit = true
			assert.Equal(t, "1.0", e.Old)
			assert.Equal(t, "2.0", e.New)
		}
	}
	assert.True(t, versionEdit, "the set literal is the carrier")
}

// The modeline is Examine's rider now rather than an edit the planner
// appends, and a bump is the intent that adopts it. It must still reach
// the plan, still as a zero-width insertion at the very top, and still
// sort ahead of everything else — the rider is folded in after the
// prediction, so this is the assertion that the move did not quietly
// drop it.
func TestBumpCarriesTheModelineRider(t *testing.T) {
	ev := newEvaluator(t)
	dir := t.TempDir()
	// No modeline, and no checksums: the rider is the only thing that
	// can put an edit at offset 0.
	portfile := `PortSystem 1.0
name unheaded
version 1.0
categories devel
maintainers nomaintainer
license MIT
description no modeline
long_description a Portfile that opens without an editor header
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(portfile), 0o644))

	p, err := Bump{Version: "2.0"}.Plan(context.Background(), handle(dir, ev), newFetcher(t))
	require.NoError(t, err)
	require.NotEmpty(t, p.Edits)
	assert.Equal(t, "modeline", p.Edits[0].Reason, "the insertion at offset 0 sorts first")
	assert.Equal(t, 0, p.Edits[0].Start)
	assert.Equal(t, 0, p.Edits[0].End)
	assert.Equal(t, intent.Modeline+"\n", p.Edits[0].New)

	// And a Portfile that already opens with one is never second-guessed.
	withHeader := filepath.Join(t.TempDir(), macports.PortfileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(withHeader), 0o755))
	require.NoError(t, os.WriteFile(withHeader, []byte(intent.Modeline+"\n"+portfile), 0o644))
	p2, err := Bump{Version: "2.0"}.Plan(context.Background(), handle(filepath.Dir(withHeader), ev), newFetcher(t))
	require.NoError(t, err)
	for _, e := range p2.Edits {
		assert.NotEqual(t, "modeline", e.Reason)
	}

	// The rider moved out of the shadowed source and into the fold that
	// happens after it, so the question this pins is whether the
	// prediction still says the same thing either way. It must: a
	// leading comment cannot change what a Portfile evaluates to, and
	// the two runs above differ in exactly that comment. The gate that
	// compares plan bytes across real ports cannot ask this — every port
	// in its table already carries a modeline, so no capture it takes
	// contains a rider at all.
	assert.Equal(t, p2.Predicted, p.Predicted,
		"the rider is folded in after the shadow, so it must not perturb the prediction")
	assert.Equal(t, p2.Summary, p.Summary)
	assert.Equal(t, p2.Slug, p.Slug)
	rest := make([]string, 0, len(p.Edits))
	for _, e := range p.Edits[1:] {
		rest = append(rest, e.Reason+": "+e.Old+" -> "+e.New)
	}
	other := make([]string, 0, len(p2.Edits))
	for _, e := range p2.Edits {
		other = append(other, e.Reason+": "+e.Old+" -> "+e.New)
	}
	assert.Equal(t, other, rest, "the rider is the only difference the missing header makes")
}

// A forced re-derivation that would fetch nothing and move no version
// has nothing to re-derive, and a plan it produced could be
// contradicted by nothing. Before the shared tail it emitted an empty
// plan and exited successfully; now it declines — in the declined band,
// in the port's own terms — rather than reaching the tail's witness
// backstop, which would land in the failure band with a sentence about
// dockhand's own falsifiability rule.
//
// Both spellings of the port are held to it. The one that already opens
// with a modeline has no rider to offer, and the one that does not is
// the case that would otherwise slip through: a rider is not a bump,
// and a branch whose entire content is an editor header is not what
// --recheck was asked for.
//
// The two also part company on the exit code, and that is the point of
// the pair. A decline that held a rider back is not the same answer as
// one that had nothing to hold: the port is where it was asked to be
// either way, and only one of them cost something. A caller sweeping
// ports reads 12 and knows to come back with --riders.
func TestBumpForceWithoutAFetchOrAMoveDeclines(t *testing.T) {
	ev := newEvaluator(t)
	body := `PortSystem 1.0
name inert
version 1.0
categories devel
maintainers nomaintainer
license MIT
description nothing to fetch
long_description a port recording no checksums at all
`
	for _, tc := range []struct {
		name     string
		portfile string
		riders   intent.RiderPolicy
		withheld []string
		exit     int
	}{
		{"with a modeline", intent.Modeline + "\n" + body, intent.RidersAlong, nil, exitcode.PlanDeclined},
		{"without one, so a rider is on offer", body, intent.RidersAlong, []string{"modeline"}, exitcode.AlreadyCurrent},
		{"without one, and --no-riders", body, intent.RidersNone, nil, exitcode.PlanDeclined},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(tc.portfile), 0o644))

			p, err := Bump{Version: "1.0", Force: true, Riders: tc.riders}.
				Plan(context.Background(), handle(dir, ev), newFetcher(t))
			require.Nil(t, p)
			var d *plan.Decline
			require.ErrorAs(t, err, &d)
			assert.Equal(t, plan.AlreadyCurrent, d.Type)
			assert.Contains(t, d.Detail, "inert records no checksums")
			assert.Equal(t, tc.withheld, d.Withheld)
			assert.Equal(t, tc.exit, d.DockhandExit(),
				"a judgment about the port, not a malfunction")
			assert.NotErrorIs(t, err, intent.ErrNoWitness,
				"the intent answers for itself; the backstop is for an intent that does not")
		})
	}
}

func TestBumpDeclinesComputedVersion(t *testing.T) {
	// Genuinely computed: the set literal is a fragment of the value,
	// so nothing corroborates and no literal candidate remains for the
	// probe.
	ev := newEvaluator(t)
	dir := t.TempDir()
	portfile := `PortSystem 1.0
name computed
set v 1.0
version ${v}.1
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
func newFetcher(t *testing.T) *portfetch.Fetcher { return porttest.Fetcher(t) }

// --recheck plans at the version the port already carries. The version
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
	err := Bump{Version: "1.0", Force: true}.accept(vals, moved, false, true)
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.UnexpectedChange, d.Type)
}

// And an ordinary bump still requires the version to arrive.
func TestAcceptOrdinaryBumpRequiresTheVersionToMove(t *testing.T) {
	vals := info.Values{Name: "foo", Version: "1.0"}
	err := Bump{Version: "2.0"}.accept(vals, info.Delta{}, true, true)
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.TargetNotReached, d.Type)
}

// A transformed carrier is judged by movement, not equality: the
// evaluated version is the Portfile's transform of the literal, known
// only to evaluation — so any nonempty new value the shadow reports is
// the target arriving, and no movement at all still declines.
func TestAcceptTransformedCarrierRequiresMovementNotEquality(t *testing.T) {
	vals := info.Values{Name: "foo", Version: "20260810.1"}
	key := info.SubportKey{Subport: "foo"}
	moved := info.Delta{Changed: map[info.SubportKey][]info.FieldChange{
		key: {{Field: info.FieldVersion, Old: []string{"20260810.1"}, New: []string{"20260824"}}},
	}}
	require.NoError(t, Bump{Version: "2026-08-24"}.accept(vals, moved, true, false),
		"the evaluated value differs from the carrier target by design")

	err := Bump{Version: "2026-08-24"}.accept(vals, info.Delta{}, true, false)
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.TargetNotReached, d.Type)
}

func TestZigVendorHashFindsThePinnedShape(t *testing.T) {
	src := []byte(`PortGroup   zig 1.0
set vaxis_commit  a367b89da09bfe5e1b628501940de5b4f858f5f3
set vaxis_hash    vaxis-0.6.0-BWNV_HHwCQB451KS7A8SMykALblPmGwHnzSfiJHjN3_9
`)
	assert.Equal(t, "vaxis-0.6.0-BWNV_HHwCQB451KS7A8SMykALblPmGwHnzSfiJHjN3_9",
		zigVendorHash(src))
}

func TestZigVendorHashLeavesOrdinaryPortsAlone(t *testing.T) {
	// A distname, a sha256, a version — none carry the 30+ character
	// base64url fingerprint after a semver.
	assert.Empty(t, zigVendorHash([]byte(`name zig
version 0.13.0
distname zig-macos-aarch64-0.13.0
checksums sha256 8e60d3430d3a69478ad0993f19238d2df97c507009a52b3c10addcd7f6bcb916
`)))
	// The hash shape without any zig context does not decline.
	assert.Empty(t, zigVendorHash([]byte(`something-1.0.0-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA`)))
}
