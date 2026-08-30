package vendored

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/portstyle"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

func TestOwnSubtractsSuppliedAndStripsTags(t *testing.T) {
	distfiles := []string{
		"tokei-13.0.0.tar.gz",
		"aho-corasick-1.1.3.crate:crate-aho-corasick-8e60d34",
		"bitflags-2.6.0.crate:crate-bitflags-b048fb6",
	}
	own, err := Own(distfiles, []string{"aho-corasick-1.1.3.crate", "bitflags-2.6.0.crate"})
	require.NoError(t, err)
	assert.Equal(t, []string{"tokei-13.0.0.tar.gz"}, own)
}

// A port's own distfile may itself carry a fetch-group tag, which is why
// tagging cannot be the test for what a block supplied.
func TestOwnKeepsTaggedDistfilesOfThePortItself(t *testing.T) {
	own, err := Own(
		[]string{"src-1.0.tar.gz:source", "docs-1.0.tar.gz:docs", "libc-0.2.156.crate:crate-libc-a5f43f1"},
		[]string{"libc-0.2.156.crate"},
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"src-1.0.tar.gz", "docs-1.0.tar.gz"}, own)
}

func TestOwnReportsSuppliedNameThePortDoesNotHave(t *testing.T) {
	_, err := Own([]string{"src-1.0.tar.gz"}, []string{"libc-0.2.156.crate"})
	require.ErrorIs(t, err, ErrUnaccounted)
	assert.Contains(t, err.Error(), "libc-0.2.156.crate")
}

func TestOwnPreservesOrderAndDuplicates(t *testing.T) {
	own, err := Own([]string{"a.crate:t1", "src.tar.gz", "a.crate:t2"}, []string{"a.crate", "a.crate"})
	require.NoError(t, err)
	assert.Equal(t, []string{"src.tar.gz"}, own)
}

const cargoPortfile = `PortSystem 1.0
name                demo
version             1.0

cargo.crates \
    aho-corasick    1.1.3   8e60d34 \
    bitflags        2.6.0   b048fb6

checksums           rmd160 aaa sha256 bbb size 1
`

func locate(t *testing.T, src string, k Kind) (text.Span, error) {
	t.Helper()
	b := []byte(src)
	cst, errs := syntax.Parse(b)
	require.Empty(t, errs)
	return Locate(b, cst, portstyle.ScopeOf(b, "demo"), k)
}

func TestLocateSpansTheWholeContinuedCommand(t *testing.T) {
	span, err := locate(t, cargoPortfile, CargoCrates)
	require.NoError(t, err)
	got := span.Text([]byte(cargoPortfile))
	assert.Contains(t, got, "cargo.crates")
	assert.Contains(t, got, "bitflags        2.6.0   b048fb6", "span must cover every continuation line")
	assert.NotContains(t, got, "checksums", "span must stop at the command's end")
}

func TestLocateAbsentBlock(t *testing.T) {
	_, err := locate(t, cargoPortfile, GoVendors)
	require.ErrorIs(t, err, ErrNoBlock)
}

// No port in the tree contributes to a block twice today. If one appears,
// replacing a single command would leave the other in place and state the
// dependencies twice, so it is refused.
func TestLocateRefusesMultipleContributingCommands(t *testing.T) {
	_, err := locate(t, cargoPortfile+"\ncargo.crates-append libc 0.2.156 a5f43f1\n", CargoCrates)
	require.ErrorIs(t, err, ErrMultipleBlocks)
}

func TestKindStringIsTheOptionName(t *testing.T) {
	assert.Equal(t, "cargo.crates", CargoCrates.String())
	assert.Equal(t, "go.vendors", GoVendors.String())
}

func TestValidateBlockAcceptsABlock(t *testing.T) {
	got, err := ValidateBlock([]byte("cargo.crates \\\n    libc 0.2.156 a5f43f1\n"), CargoCrates)
	require.NoError(t, err)
	assert.Equal(t, "cargo.crates \\\n    libc 0.2.156 a5f43f1", string(got),
		"trailing newline is trimmed: a located span does not include it")
}

// Both generators can produce nothing while exiting zero.
func TestValidateBlockRejectsSilentEmptyOutput(t *testing.T) {
	_, err := ValidateBlock([]byte("No packages with checksums found.\n"), CargoCrates)
	require.ErrorIs(t, err, ErrEmptyBlock)
	assert.Contains(t, err.Error(), "No packages with checksums found.")
}

func TestValidateBlockRejectsOutputOfTheWrongKind(t *testing.T) {
	_, err := ValidateBlock([]byte("cargo.crates \\\n    libc 0.2.156 a5f43f1\n"), GoVendors)
	require.ErrorIs(t, err, ErrEmptyBlock)
}

func TestEditReplacesTheLocatedSpan(t *testing.T) {
	span, err := locate(t, cargoPortfile, CargoCrates)
	require.NoError(t, err)
	edit := Edit([]byte(cargoPortfile), span, []byte("cargo.crates \\\n    libc 0.2.156 a5f43f1"), CargoCrates)
	assert.Equal(t, span.Start, edit.Start)
	assert.Equal(t, span.End, edit.End)
	assert.Contains(t, edit.Old, "aho-corasick")
	assert.Contains(t, edit.New, "libc")
	assert.Equal(t, "regenerate cargo.crates", edit.Reason)
}
