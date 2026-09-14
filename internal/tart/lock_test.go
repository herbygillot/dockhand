package tart

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestImageLocksAllowReadersAndExcludeWriter(t *testing.T) {
	home := t.TempDir()
	first, err := AcquireImageRead(t.Context(), home, "base")
	require.NoError(t, err)
	defer first.Close()
	second, err := AcquireImageRead(t.Context(), home, "base")
	require.NoError(t, err)
	require.NoError(t, second.Close())

	ctx, cancel := context.WithTimeout(t.Context(), 75*time.Millisecond)
	defer cancel()
	_, err = AcquireImageWrite(ctx, home, "base")
	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded))

	require.NoError(t, first.Close())
	writer, err := AcquireImageWrite(t.Context(), home, "base")
	require.NoError(t, err)
	require.NoError(t, writer.Close())
}
