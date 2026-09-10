package bump

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/port/porttest"
)

// The consequence of the write, read from the evaluation that proved
// the carrier. This is the terraform shape reduced to its bones: a
// version composed from a fragment, and a checksums block naming two
// files whose names the fragment drives.
//
// What the Result carries is the point. Whether the amd64 key renames
// while its digests stay put is a question about a tree that does not
// exist yet, and text alone can only guess at it; here the interpreter
// has already answered.
func TestCarrierResultCarriesTheWholeConsequence(t *testing.T) {
	src := []byte(`PortSystem 1.0
name resultprobe
set patchNumber      3
version              1.16.${patchNumber}
distname             resultprobe_${version}_darwin_arm64
checksums            resultprobe_${version}_darwin_amd64.zip \
                     rmd160 aa sha256 bb size 1 \
                     resultprobe_${version}_darwin_arm64.zip \
                     rmd160 cc sha256 dd size 2
`)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Portfile"), src, 0o644))
	h := porttest.Handle(porttest.Evaluator(t), dir)

	ctx := context.Background()
	s, cst, err := h.Source()
	require.NoError(t, err)
	vals, err := h.Values(ctx)
	require.NoError(t, err)
	require.Equal(t, "1.16.3", vals.Version)

	c, ok := discover(ctx, h, s, cst, vals, "1.16.9")
	require.True(t, ok, "the version fragment is the carrier")
	assert.Equal(t, "9", c.Write)
	assert.Equal(t, `"1.16." + <3> + ""`, c.Template)

	// The version is the target because discover accepted no carrier
	// that failed to deliver one — not because arithmetic said so.
	assert.Equal(t, "1.16.9", c.Result.Version)
	// And the rest of the consequence: the distfile the write renames,
	// and both checksum keys moving with it, including the one for a
	// file this context never fetches.
	assert.Equal(t, []string{"resultprobe_1.16.9_darwin_arm64.tar.gz"}, c.Result.Distfiles)
	assert.Contains(t, c.Result.Checksums, "resultprobe_1.16.9_darwin_amd64.zip")
	assert.Contains(t, c.Result.Checksums, "resultprobe_1.16.9_darwin_arm64.zip")
}

// A carrier that reaches the version by anything other than
// concatenation passes the affix arithmetic on one probe and lands
// somewhere else on the write. This is what the confirming evaluation
// is for.
func TestCarrierConfirmRefusesACarrierThatDoesNotDeliver(t *testing.T) {
	// `part` composes the version, and the affix test on a same-shape
	// probe proves "9." + <part> + "" — but the Portfile pads what it
	// writes to two digits, so writing "7" to reach 9.7 yields 9.07.
	src := []byte(`PortSystem 1.0
name padprobe
set part             12
version              9.[format %02d ${part}]
checksums            rmd160 0 sha256 0 size 0
`)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Portfile"), src, 0o644))
	h := porttest.Handle(porttest.Evaluator(t), dir)

	ctx := context.Background()
	s, cst, err := h.Source()
	require.NoError(t, err)
	vals, err := h.Values(ctx)
	require.NoError(t, err)
	require.Equal(t, "9.12", vals.Version)

	require.Len(t, prove(ctx, h, s, cst, vals, "9.7"), 1, "the arithmetic proves it")
	_, ok := discover(ctx, h, s, cst, vals, "9.7")
	assert.False(t, ok, "and the confirming evaluation throws it out")
}
