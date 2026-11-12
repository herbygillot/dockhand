package buildcmd_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/buildcmd"
	"github.com/herbygillot/dockhand/internal/buildenv/starter"
	"github.com/herbygillot/dockhand/internal/model"
)

// free is a launch lock nothing holds.
type free struct{}

func (free) Hold(context.Context, time.Duration) (func(), error) { return func() {}, nil }

// A script's stop finds its starter by the directory the attempt noted,
// whatever reference the script's result gave: a record from another boot
// is ended; an attempt from before notes names no shell, and isn't known;
// one that launched nothing is ended (the lifecycle design's "Script").
func TestAScriptsStopReadsTheDirectoryItsAttemptNoted(t *testing.T) {
	t.Parallel()
	p := &buildcmd.Provider{Grace: time.Second}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, starter.RecordName), []byte(`{"pid":1,"start":"x","boot":"another boot"}`), 0o644))
	_, err := p.Stop(t.Context(), buildenv.StopRequest{Execution: model.GuestExecution{Attempt: 1, ProviderRef: "box-17", Launch: `{"directory":"` + dir + `"}`}, Lock: free{}})
	require.NoError(t, err)

	_, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: model.GuestExecution{Attempt: 2, ProviderRef: "box-17"}, Lock: free{}})
	require.ErrorContains(t, err, "attempt 2 was started before dockhand recorded where, so nothing names its shell to stop")

	_, err = p.Stop(t.Context(), buildenv.StopRequest{Execution: model.GuestExecution{Attempt: 3}, Lock: free{}})
	require.NoError(t, err)
}
