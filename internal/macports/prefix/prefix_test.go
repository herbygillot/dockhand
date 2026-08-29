package prefix

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

func fakeLookPath(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	orig := lookPath
	lookPath = fn
	t.Cleanup(func() { lookPath = orig })
}

// installed builds a directory that passes for an installation.
func installed(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bin", macports.TclShellName), []byte("#!"), 0o755))
	return dir
}

func TestPrefixPaths(t *testing.T) {
	p := Prefix("/opt/dockhand/verify")
	assert.Equal(t, "/opt/dockhand/verify/bin/port-tclsh", p.PortTclsh())
	assert.Equal(t, "/opt/dockhand/verify/bin/port", p.Port())
}

func TestNewValidates(t *testing.T) {
	dir := installed(t)
	p, err := New(dir)
	require.NoError(t, err)
	assert.Equal(t, Prefix(dir), p)

	// A stated prefix is validated, never fallen back from.
	_, err = New(t.TempDir())
	require.ErrorIs(t, err, ErrNotInstalled)
}

func TestFindViaPath(t *testing.T) {
	fakeLookPath(t, func(string) (string, error) {
		return "/opt/somewhere/bin/port-tclsh", nil
	})
	p, err := find("/nonexistent")
	require.NoError(t, err)
	assert.Equal(t, Prefix("/opt/somewhere"), p)
}

func TestFindViaDefaultPrefix(t *testing.T) {
	fakeLookPath(t, func(string) (string, error) {
		return "", errors.New("not on PATH")
	})
	dir := installed(t)
	p, err := find(dir)
	require.NoError(t, err)
	assert.Equal(t, Prefix(dir), p)
}

func TestFindNotInstalled(t *testing.T) {
	fakeLookPath(t, func(string) (string, error) {
		return "", errors.New("not on PATH")
	})
	_, err := find(t.TempDir())
	require.ErrorIs(t, err, ErrNotInstalled)
}
