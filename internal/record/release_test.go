package record

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The Selection is embedded so that stored releases keep their flat shape.
func TestReleaseSelectionIsStoredFlat(t *testing.T) {
	release := Release{Selection: Selection{Requested: "2.0", CurrentVersion: "1.0", Stability: "stable"}, Version: "2.0", Tag: "v2.0"}
	raw, err := json.Marshal(release)
	require.NoError(t, err)
	var flat map[string]any
	require.NoError(t, json.Unmarshal(raw, &flat))
	require.Equal(t, "2.0", flat["Requested"])
	require.Equal(t, "1.0", flat["CurrentVersion"])
	require.NotContains(t, flat, "Selection")
	var back Release
	require.NoError(t, json.Unmarshal(raw, &back))
	require.Equal(t, release, back)
}
