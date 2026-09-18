package version

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTagAndStringForms(t *testing.T) {
	t.Parallel()
	require.Equal(t, "v0.3.0", Info{Version: "v0.3.0", Revision: "1a2b3c4d5e6f"}.Tag())
	require.Equal(t, "v0.3.0 (1a2b3c4d5e6f)", Info{Version: "v0.3.0", Revision: "1a2b3c4d5e6f"}.String())
	require.Equal(t, "devel+1a2b3c4d5e6f", Info{Version: "devel", Revision: "1a2b3c4d5e6f"}.Tag())
	require.Equal(t, "devel+1a2b3c4d5e6f.modified", Info{Version: "devel", Revision: "1a2b3c4d5e6f", Modified: true}.Tag())
	require.Equal(t, "devel (1a2b3c4d5e6f, modified)", Info{Version: "devel", Revision: "1a2b3c4d5e6f", Modified: true}.String())
	require.Equal(t, "devel", Info{Version: "devel"}.Tag())
	require.NotEmpty(t, Current().Tag())
}
