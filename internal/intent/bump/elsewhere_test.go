package bump

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/macports/port"
	"github.com/herbygillot/dockhand/internal/macports/port/porttest"
)

// ask parses the source and reads the port, then puts the question the
// way bump does at the point of refusal.
func ask(t *testing.T, h port.Handle, src []byte) string {
	t.Helper()
	s, cst, err := h.Source()
	require.NoError(t, err)
	require.Equal(t, src, s)
	var vals info.Values
	vals, err = h.Values(context.Background())
	require.NoError(t, err)
	return elsewhere(context.Background(), h, src, cst, vals)
}

// A carrier composed from the subport's own name is the case the text
// cannot state: "2.9" is in the interpreter and nowhere in the bytes.
func TestElsewhereNamesACarrierOutsideTheFile(t *testing.T) {
	src := []byte(`PortSystem 1.0
name subportprobe
version 0
checksums rmd160 0 sha256 0 size 0
subport subportprobe-2.9 {
    set baseVersion [lindex [split ${subport} "-"] 1]
    version ${baseVersion}.4
}
`)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Portfile"), src, 0o644))
	h := porttest.Handle(porttest.Evaluator(t), dir).Subport("subportprobe-2.9")

	got := ask(t, h, src)
	assert.Contains(t, got, `baseVersion = "2.9"`)
	assert.Contains(t, got, "not a version literal in this Portfile")
}

// A carrier the file DOES hold was already offered to the locator;
// repeating it as a discovery would be noise.
func TestElsewhereSaysNothingWhenTheCarrierIsLocal(t *testing.T) {
	src := []byte(`PortSystem 1.0
name localprobe
set patchNumber 17
version 1.2.${patchNumber}
checksums rmd160 0 sha256 0 size 0
`)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Portfile"), src, 0o644))
	h := porttest.Handle(porttest.Evaluator(t), dir)

	assert.Empty(t, ask(t, h, src), "the `set` span is a candidate the locator already offered")
}

// An oracle with no interpreter returns an error, and the sentence is
// simply absent: this enriches a refusal and must never make one.
func TestElsewhereIsSilentWithoutAnInterpreter(t *testing.T) {
	h := porttest.Handle(&porttest.Oracle{}, t.TempDir())
	assert.Empty(t, elsewhere(context.Background(), h, []byte("version 1.0\n"), nil,
		info.Values{Semantic: info.Semantic{Version: "1.0"}}))
}
