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

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/tool"
)

// twoArchPort is the terraform shape reduced to its bones: distname is
// set twice so distfiles offers only arm64, while the checksums command
// records both architectures. A bump has to move four digests and can
// fetch one file from distfiles.
func twoArchPort(t *testing.T, siteURL string, amd, arm []byte) string {
	t.Helper()
	sum := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	dir := t.TempDir()
	portfile := fmt.Sprintf(`# -*- coding: utf-8; mode: tcl -*-
PortSystem 1.0
name twoarch
version 1.0
revision 0
categories devel
maintainers nomaintainer
license MIT
description synthetic two-architecture target
long_description synthetic two-architecture target for dockhand tests
master_sites %s
checksums twoarch_${version}_amd64.zip \
          rmd160 00000000000000000000000000000000000000a1 \
          sha256 %s \
          size %d \
          twoarch_${version}_arm64.zip \
          rmd160 00000000000000000000000000000000000000b2 \
          sha256 %s \
          size %d
distname twoarch_${version}_amd64
distname twoarch_${version}_arm64
`, siteURL, sum(amd), len(amd), sum(arm), len(arm))
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(portfile), 0o644))
	return dir
}

// The whole point: the architecture this evaluation never fetches gets
// its digests re-derived rather than left describing the old release.
func TestBumpRederivesTheArchitectureItDoesNotFetch(t *testing.T) {
	ev := newEvaluator(t)
	srv := distServer(t)
	amd := servedFor("/dist/twoarch_1.0_amd64.zip")
	arm := servedFor("/dist/twoarch_1.0_arm64.zip")
	dir := twoArchPort(t, srv.URL+"/dist", amd, arm)

	b := Bump{Version: "2.0"}
	p, err := b.Plan(context.Background(), handle(dir, ev), newFetcher(t))
	require.NoError(t, err, "this is the plan that used to be refused")

	// Both architectures moved, so every recorded value has somewhere to
	// go: two of each digest type rather than one.
	reasons := map[string]int{}
	for _, e := range p.Edits {
		reasons[e.Reason]++
	}
	assert.Equal(t, 2, reasons["checksum rmd160"], "one per architecture")
	assert.Equal(t, 2, reasons["checksum sha256"], "one per architecture")
	assert.Equal(t, 2, reasons["checksum size"], "one per architecture")

	// And the values are the ones the served bytes actually hash to,
	// which is the claim the plan is making about a file this run never
	// builds with.
	want := sha256.Sum256(servedFor("/dist/twoarch_2.0_amd64.zip"))
	found := false
	for _, e := range p.Edits {
		if e.New == hex.EncodeToString(want[:]) {
			found = true
		}
	}
	assert.True(t, found, "the amd64 digest is the digest of the amd64 bytes at 2.0")
}

// A file whose name does not move is a pin, and its digests are correct
// as they stand: nothing is fetched for it and nothing is rewritten.
func TestNamedNotFetchedSkipsPins(t *testing.T) {
	before := []string{"moves-1.0.zip", "pinned-3.3.zip"}
	after := []string{"moves-2.0.zip", "pinned-3.3.zip"}
	named := map[string][]string{"moves-2.0.zip": {"http://example.invalid/moves-2.0.zip"}}

	oldNames, newNames, ok := rederivable(before, after, named)
	require.True(t, ok)
	assert.Equal(t, []string{"moves-1.0.zip"}, oldNames)
	assert.Equal(t, []string{"moves-2.0.zip"}, newNames)
}

// A moving file with nowhere to fetch it from keeps the refusal: every
// master_sites entry tagged means MacPorts binds files to sites through
// distfiles, and a file absent from distfiles has no binding to read.
func TestRederivableRefusesWhenAFileHasNoSite(t *testing.T) {
	_, _, ok := rederivable(
		[]string{"a-1.0.zip", "b-1.0.zip"},
		[]string{"a-2.0.zip", "b-2.0.zip"},
		map[string][]string{"a-2.0.zip": {"http://example.invalid/a-2.0.zip"}},
	)
	assert.False(t, ok, "b-2.0.zip has no url, so nothing here is re-derived")
}

// Pairing by position is only sound while the command's shape holds.
func TestRederivableRefusesAChangeOfShape(t *testing.T) {
	_, _, ok := rederivable([]string{"a-1.0.zip"}, []string{"a-2.0.zip", "b-2.0.zip"}, nil)
	assert.False(t, ok)
}

// Vendored files belong to their block, which regenerates them whole.
func TestNamedNotFetchedSubtractsSuppliedAndFetched(t *testing.T) {
	toks := strings.Fields("own.zip rmd160 a sha256 b size 1 fetched.zip rmd160 c sha256 d size 2 crate.crate rmd160 e sha256 f size 3")
	got := namedNotFetched(toks, []string{"fetched.zip"}, []string{"crate.crate"})
	assert.Equal(t, []string{"own.zip"}, got)
}

// A RE-DERIVED FILE IS FETCHED FOR ITS DIGEST AND MUST NOT BECOME A
// PLACE A PATCH TARGET IS READ FROM. The companion served here holds
// the patch's target at the very path the real source does, with
// different content: texlive names a -src archive beside the -run one
// it builds, and both can carry a path the other has. A relocation
// that read the companion would mint a patch whose phase fails against
// what the port actually extracts.
func TestRederivedFilesAreNotSearchedForPatchTargets(t *testing.T) {
	testenv.Tool(t, "tar")
	ev := newEvaluator(t)

	// The release DROPPED the patch's target, and the companion still
	// carries a file at the same path. Order alone does not save this:
	// the real archive is searched first and simply has nothing, so
	// whatever is searched next answers.
	real20 := tarball(t, map[string]string{
		"companion-2.0/prog.c": "int main(void) { return 0; }\n",
	})
	decoy20 := tarball(t, map[string]string{
		"companion-2.0/Makefile": "DECOY\n" + makefile,
	})
	real10 := tarball(t, map[string]string{"companion-1.0/Makefile": makefile})
	decoy10 := tarball(t, map[string]string{"companion-1.0/Makefile": "DECOY\n" + makefile})

	pick := func(path string) []byte {
		newer := strings.Contains(path, "2.0")
		if strings.Contains(path, "-doc") {
			if newer {
				return decoy20
			}
			return decoy10
		}
		if newer {
			return real20
		}
		return real10
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pick(r.URL.Path))
	}))
	t.Cleanup(srv.Close)

	sum := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	dir := t.TempDir()
	portfile := fmt.Sprintf(`# -*- coding: utf-8; mode: tcl -*-
PortSystem 1.0
name companion
version 1.0
revision 0
categories devel
maintainers nomaintainer
license MIT
description synthetic companion-archive target
long_description synthetic companion-archive target for dockhand tests
master_sites %%s
distfiles companion-${version}.tar.gz
worksrcdir companion-${version}
patchfiles %s
checksums companion-${version}.tar.gz \
          rmd160 00000000000000000000000000000000000000a1 \
          sha256 %%s \
          size %%d \
          companion-${version}-doc.tar.gz \
          rmd160 00000000000000000000000000000000000000b2 \
          sha256 %%s \
          size %%d
`, patchName)
	portfile = fmt.Sprintf(portfile, srv.URL+"/dist",
		sum(real10), len(real10), sum(decoy10), len(decoy10))
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(portfile), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "files"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "files", patchName), []byte(patchBody), 0o644))

	p, err := Bump{Version: "2.0", Tools: tool.NewFinder(nil)}.
		Plan(context.Background(), handle(dir, ev), newFetcher(t))
	require.NoError(t, err)

	// The companion's digests moved, so it WAS fetched.
	reasons := map[string]int{}
	for _, e := range p.Edits {
		reasons[e.Reason]++
	}
	assert.Equal(t, 2, reasons["checksum sha256"], "both files were hashed")

	// And the patch was NOT relocated: its target is gone from the
	// source, which is a question for a person. Reading it out of the
	// companion would have produced a confident, wrong answer instead.
	assert.Empty(t, p.Files, "nothing may be read out of a digest-only companion")
	kinds := map[string]int{}
	for _, f := range p.Findings {
		kinds[f.Kind]++
	}
	assert.NotZero(t, kinds[FindingPatchUnrelocated],
		"the missing target is reported rather than answered from the wrong archive")
}
