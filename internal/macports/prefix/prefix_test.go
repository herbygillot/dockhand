package prefix

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tool"
)

// finder builds the run's tool finder over a fake PATH search, so
// discovery is tested against a stated machine rather than this one.
func finder(fn func(string) (string, error)) *tool.Finder {
	return tool.NewFinder(fn)
}

// installed builds a directory that passes for an installation.
func installed(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bin", string(tool.PortTclsh)), []byte("#!"), 0o755))
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
	tools := finder(func(string) (string, error) {
		return "/opt/somewhere/bin/port-tclsh", nil
	})
	p, err := find(tools, "/nonexistent")
	require.NoError(t, err)
	assert.Equal(t, Prefix("/opt/somewhere"), p)
}

func TestFindViaDefaultPrefix(t *testing.T) {
	tools := finder(func(string) (string, error) {
		return "", errors.New("not on PATH")
	})
	dir := installed(t)
	p, err := find(tools, dir)
	require.NoError(t, err)
	assert.Equal(t, Prefix(dir), p)
}

func TestFindNotInstalled(t *testing.T) {
	tools := finder(func(string) (string, error) {
		return "", errors.New("not on PATH")
	})
	_, err := find(tools, t.TempDir())
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

// A failing port client is reported in os/exec's words, not its own:
// Version has always wrapped "exit status N", and the seam's failure
// type stops at the seam.
func TestVersionWrapsExecsOwnWordsOnFailure(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "bin"), 0o755))
	port := filepath.Join(dir, "bin", "port")
	require.NoError(t, os.WriteFile(port, []byte("#!/bin/sh\necho 'Error: port version failed' >&2\nexit 1\n"), 0o755))

	_, err := runVersion(context.Background(), port, "version")
	require.Error(t, err)
	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee)
	var f *tool.Failure
	assert.NotErrorAs(t, err, &f, "the seam's failure type does not reach the caller")

	_, err = Prefix(dir).Version(context.Background())
	require.ErrorAs(t, err, &ee)
	assert.Equal(t, "prefix: "+port+" version: exit status 1", err.Error())
}
