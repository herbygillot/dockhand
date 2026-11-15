package clean

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/engine"
)

// A listing problem is said in the preview, and verbose completion does
// not repeat it or count it as a resource it removed (#241).
func TestCleanReportsAListingProblemOnce(t *testing.T) {
	t.Parallel()
	problem := buildenv.Leftover{What: "GitHub check branches", Kept: "couldn't list them: GitHub unreachable"}
	leftovers := []engine.Leftover{{Leftover: problem, Kept: problem.Kept}}
	var out bytes.Buffer
	require.Zero(t, writeLeftovers(&out, leftovers, false))
	require.Zero(t, writeLeftovers(&out, leftovers, true))
	require.Equal(t, 1, strings.Count(out.String(), "GitHub unreachable"))
}
