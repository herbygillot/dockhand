//go:build !acceptance

package failpoint_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/failpoint"
)

// A normal build has no failpoints: Hit does nothing, and the release
// binary doesn't name the variable that would set one.
func TestANormalBuildHasNoFailpoints(t *testing.T) {
	require.False(t, failpoint.Enabled)
	t.Setenv("DOCKHAND_FAILPOINT", "tidy.prepared:kill")
	failpoint.Hit("tidy.prepared")

	binary := filepath.Join(t.TempDir(), "dockhand")
	build := exec.Command("go", "build", "-o", binary, "github.com/herbygillot/dockhand/cmd/dockhand")
	build.Env = append(os.Environ(), "GOFLAGS=-mod=vendor")
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
	data, err := os.ReadFile(binary)
	require.NoError(t, err)
	require.False(t, bytes.Contains(data, []byte("DOCKHAND_FAILPOINT")), "the release binary names no failpoint")
}
