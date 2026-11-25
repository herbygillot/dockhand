package filedigest

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSumReturnsSizeAndSHA256(t *testing.T) {
	t.Parallel()
	got, err := Sum(context.Background(), strings.NewReader("dockhand"))
	require.NoError(t, err)
	require.Equal(t, int64(8), got.Size)
	require.Equal(t, "53401fb38901a35a106e1d8cfbbac53615d3b4b08261999709a4b39ddc3bb12d", got.SHA256)
}

func TestSumHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Sum(ctx, strings.NewReader("bytes"))
	require.ErrorIs(t, err, context.Canceled)
}
