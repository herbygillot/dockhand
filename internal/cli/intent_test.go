package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/app"
)

// THE ONE INTENT WHOSE CHANGE LEAVES THE VERSION WHERE IT WAS is spelled
// twice — once here as the verb a person types and the value that lands
// in a plan's Intent field, and once in internal/app, which reads it off
// a subject to decide that the port's binary archive must be ignored.
//
// app must not import this package (the catalogue is the CLI's), so the
// two spellings can only be held together by a test. Without one, a
// rename of the verb would silently stop `refresh-checksums` building
// from source — and verifying re-derived checksums against the archive
// of the bytes they replaced is the one thing that flag exists to
// prevent.
func TestTheRefreshIntentIsSpelledTheSameWayAppReadsIt(t *testing.T) {
	var names []string
	for _, v := range intentCatalogue() {
		names = append(names, v.Name)
	}
	require.Contains(t, names, app.IntentRefresh,
		"app decides from this string; the catalogue is where it is defined")
	assert.Equal(t, "refresh-checksums", app.IntentRefresh)
}
