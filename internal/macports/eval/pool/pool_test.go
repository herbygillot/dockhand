package pool

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/prefix"
	"github.com/herbygillot/dockhand/internal/testenv"
)

func portdirWith(t *testing.T, portfile string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte(portfile), 0o644))
	return dir
}

func TestPoolLifecycle(t *testing.T) {
	ctx := context.Background()
	p, err := New(ctx, testenv.MacPortsPrefix(t), 2)
	require.NoError(t, err)
	defer p.Close()
	evs := p.Evaluators()
	require.Len(t, evs, 2)

	dir := portdirWith(t, "PortSystem 1.0\nname pooltest\nversion 1.0\n")
	v, err := evs[0].Top(ctx, dir, "")
	require.NoError(t, err)
	require.Equal(t, "pooltest", v.Name)

	// Replacement closes the old evaluator and keeps the pool at size.
	fresh, err := p.Replace(evs[0])
	require.NoError(t, err)
	require.Len(t, p.Evaluators(), 2)
	v, err = fresh.Top(ctx, dir, "")
	require.NoError(t, err)
	require.Equal(t, "pooltest", v.Name)
}

func TestPoolNoEvaluator(t *testing.T) {
	_, err := New(context.Background(), prefix.Prefix("/nonexistent"), 2)
	require.Error(t, err)
	require.ErrorContains(t, err, "no evaluator could be started")
	// Whatever the underlying cause, the error carries the startup
	// identity the exit table classifies on.
	require.ErrorIs(t, err, eval.ErrStartup)
}
