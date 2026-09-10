package bump

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// The LyX shape: two checksums commands in two branches of one `if`,
// holding byte-identical digests. rewrite.Edits locates by value and
// takes the first match, so a write meant for either would land in the
// earlier one. Measured over the tree, 6 ports are like this; every one
// of them is refused rather than written somewhere plausible.
func TestBumpRefusesADigestWrittenTwiceInOneScope(t *testing.T) {
	ev := newEvaluator(t)
	srv := distServer(t)
	content := servedFor("/dist/twinned-1.0.tar.gz")
	sum := sha256.Sum256(content)
	dir := t.TempDir()
	portfile := fmt.Sprintf(`# -*- coding: utf-8; mode: tcl -*-
PortSystem 1.0
name twinned
version 1.0
revision 0
categories devel
maintainers nomaintainer
license MIT
description synthetic twinned-digest target
long_description synthetic twinned-digest target for dockhand tests
master_sites %s
if {${os.major} >= 1} {
    checksums rmd160 00000000000000000000000000000000000000a1 \
              sha256 %s \
              size %d
} else {
    checksums rmd160 00000000000000000000000000000000000000a1 \
              sha256 %s \
              size %d
}
`, srv.URL+"/dist", hex.EncodeToString(sum[:]), len(content), hex.EncodeToString(sum[:]), len(content))
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(portfile), 0o644))

	b := Bump{Version: "2.0"}
	_, err := b.Plan(context.Background(), handle(dir, ev), newFetcher(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "more than one checksums command",
		"the refusal says why, not just that")
}

// The same value in one command twice is the same ambiguity and gets
// the same answer.
func TestAmbiguousSeesARepeatWithinOneCommand(t *testing.T) {
	src := []byte(`PortSystem 1.0
name repeated
checksums a.zip rmd160 aa sha256 dead size 1 b.zip rmd160 bb sha256 dead size 2
`)
	cst, perrs := syntax.Parse(src)
	require.Empty(t, perrs)
	dup, ok := ambiguous(src, cst, portstyle.ScopeOf(src, "repeated"),
		[]checksums.Replacement{{Old: "dead", New: "beef"}})
	assert.True(t, ok)
	assert.Equal(t, "dead", dup)
}

// A replacement that changes nothing needs no place to go.
func TestAmbiguousIgnoresAValueThatIsNotMoving(t *testing.T) {
	src := []byte(`PortSystem 1.0
name still
checksums a.zip rmd160 aa sha256 dead size 1 b.zip rmd160 bb sha256 dead size 2
`)
	cst, perrs := syntax.Parse(src)
	require.Empty(t, perrs)
	_, ok := ambiguous(src, cst, portstyle.ScopeOf(src, "still"),
		[]checksums.Replacement{{Old: "dead", New: "dead"}})
	assert.False(t, ok)
}
