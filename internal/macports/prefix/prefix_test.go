package prefix

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/platform"
)

func fakeLookPath(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	t.Cleanup(platform.StubLookup(fn))
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

func TestVersion(t *testing.T) {
	orig := runVersion
	t.Cleanup(func() { runVersion = orig })

	runVersion = func(context.Context, string, ...string) (string, error) {
		return "Version: 2.12.6\n", nil
	}
	v, err := Prefix("/opt/local").Version(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "2.12.6", v)

	// Unexpected output is an error, not a guess.
	runVersion = func(context.Context, string, ...string) (string, error) {
		return "something else\n", nil
	}
	_, err = Prefix("/opt/local").Version(context.Background())
	require.Error(t, err)

	runVersion = func(context.Context, string, ...string) (string, error) {
		return "", errors.New("no port client")
	}
	_, err = Prefix("/opt/local").Version(context.Background())
	require.Error(t, err)
}

// A verification backend drives an installation that is not this
// machine's — inside a VM, or an ephemeral prefix made for one run — so
// path construction must work on a Prefix that was never validated.
func TestLayoutPathsNeedNoValidation(t *testing.T) {
	p := Prefix("/opt/local")
	assert.Equal(t, "/opt/local/bin/port", p.Port())
	assert.Equal(t, "/opt/local/bin/portindex", p.Portindex())
	assert.Equal(t, "/opt/local/etc/macports/sources.conf", p.SourcesConf())

	e := Prefix("/opt/dockhand/e/abc123")
	assert.Equal(t, "/opt/dockhand/e/abc123/bin/portindex", e.Portindex())
	assert.Equal(t, "/opt/dockhand/e/abc123/etc/macports/sources.conf", e.SourcesConf())
}
