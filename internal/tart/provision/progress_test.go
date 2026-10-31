package provision

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

func TestProgressRunsOnlyDuringOperation(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	writer := &progressWriter{writer: &buffer}
	failure := errors.New("fixture")
	err := reportProgress(t.Context(), writer, "Xcode installation", time.Millisecond, func() error {
		require.Eventually(t, func() bool { writer.mu.Lock(); defer writer.mu.Unlock(); return buffer.Len() > 0 }, testsupport.Settle, time.Millisecond)
		return failure
	})
	require.ErrorIs(t, err, failure)
	require.Contains(t, buffer.String(), "Xcode installation: still working")
	previous := buffer.String()
	time.Sleep(5 * time.Millisecond)
	require.Equal(t, previous, buffer.String())
}
