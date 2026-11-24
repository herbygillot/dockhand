package view

import (
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/stretchr/testify/require"
)

// ship's printer lists what holds first, then what informs, a line each,
// grouped by port in the order they came (Codex's read of 2d, P3).
func TestShipsNoticesSayWhatHoldsFirstByPort(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	WriteNotices(&out, []engine.Notice{
		{Port: "jq", Words: "a new -sys crate: openssl-sys"},
		{Port: "jq-doc", Words: "patch fix.diff doesn't apply", Holds: true},
		{Port: "jq", Words: "patch other.diff doesn't apply", Holds: true},
		{Port: "jq-doc", Words: "the Portfile says: keep at 1.x"},
		{Port: "jq", Words: "the repository is now jqlang/jq"},
	})
	require.Equal(t, "What its updates found that holds it:\n"+
		"  jq-doc: patch fix.diff doesn't apply\n"+
		"  jq: patch other.diff doesn't apply\n"+
		"What its updates found:\n"+
		"  jq: a new -sys crate: openssl-sys\n"+
		"  jq: the repository is now jqlang/jq\n"+
		"  jq-doc: the Portfile says: keep at 1.x\n", out.String())
	out.Reset()
	WriteNotices(&out, nil)
	require.Empty(t, out.String(), "nothing where there are none")
}
