package intent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

func TestChecksumEdits(t *testing.T) {
	src := []byte(`PortSystem 1.0
checksums           rmd160  aaaa \
                    sha256  bbbb \
                    size    9
`)
	cst, errs := syntax.Parse(src)
	require.Empty(t, errs)
	old := []checksums.Recorded{{Type: "rmd160", Value: "aaaa"}, {Type: "sha256", Value: "bbbb"}, {Type: "size", Value: "9"}}
	sums := map[string]checksums.Sums{
		"foo-2.0.tar.gz": {Rmd160: "cccc", Sha256: "dddd", Size: 12},
	}
	edits, err := checksumEdits(src, cst, "foo", old,
		[]string{"foo-1.0.tar.gz"}, []string{"foo-2.0.tar.gz"}, sums)
	require.NoError(t, err)
	require.Len(t, edits, 3)
	assert.Equal(t, "cccc", edits[0].New)
	assert.Equal(t, "dddd", edits[1].New)
	assert.Equal(t, "12", edits[2].New)

	// A recorded value that appears nowhere as a literal declines.
	old[0].Value = "zzzz"
	_, err = checksumEdits(src, cst, "foo", old,
		[]string{"foo-1.0.tar.gz"}, []string{"foo-2.0.tar.gz"}, sums)
	var d *Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, ChecksumsNotLocated, d.Type)
}

func TestChecksumEditsRenamesLiteralFilenames(t *testing.T) {
	src := []byte(`checksums foo-1.0.tar.gz sha256 bbbb size 9
`)
	cst, errs := syntax.Parse(src)
	require.Empty(t, errs)
	old := []checksums.Recorded{{File: "foo-1.0.tar.gz", Type: "sha256", Value: "bbbb"}, {File: "foo-1.0.tar.gz", Type: "size", Value: "9"}}
	sums := map[string]checksums.Sums{"foo-2.0.tar.gz": {Sha256: "dddd", Size: 12}}
	edits, err := checksumEdits(src, cst, "foo", old,
		[]string{"foo-1.0.tar.gz"}, []string{"foo-2.0.tar.gz"}, sums)
	require.NoError(t, err)
	require.Len(t, edits, 3)
	var renamed bool
	for _, e := range edits {
		if e.Reason == "distfile name" {
			renamed = true
			assert.Equal(t, "foo-1.0.tar.gz", e.Old)
			assert.Equal(t, "foo-2.0.tar.gz", e.New)
		}
	}
	assert.True(t, renamed, "literal filename must be renamed")
}

func TestChecksumEditsDeclinesLegacyTypes(t *testing.T) {
	src := []byte("checksums md5 ee\n")
	cst, _ := syntax.Parse(src)
	_, err := checksumEdits(src, cst, "foo",
		[]checksums.Recorded{{Type: "md5", Value: "ee"}},
		[]string{"f-1.tar.gz"}, []string{"f-2.tar.gz"},
		map[string]checksums.Sums{"f-2.tar.gz": {}})
	var d *Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, ChecksumsNotLocated, d.Type)
}
