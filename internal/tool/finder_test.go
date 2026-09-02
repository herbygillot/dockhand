package tool

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindWithMemoizesTheFallbackUnderTheTool(t *testing.T) {
	calls := 0
	f := NewFinder(func(name string) (string, error) {
		calls++
		if name == "/opt/local/bin/port-tclsh" {
			return name, nil
		}
		return "", errors.New("nope")
	})

	_, err := f.FindWith(PortTclsh, "/opt/local/bin/port-tclsh")
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "the PATH search, then the fallback")

	path, err := f.Find(PortTclsh)
	require.NoError(t, err)
	assert.Equal(t, "/opt/local/bin/port-tclsh", path, "a later Find answers with the fallback doctor found")
	assert.Equal(t, 2, calls, "and looks nothing up to do it")
}

func TestFindWithMissesWithoutAFallback(t *testing.T) {
	calls := 0
	f := NewFinder(func(string) (string, error) {
		calls++
		return "", errors.New("nope")
	})

	_, err := f.FindWith(Tclsh, "")
	require.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, 1, calls, "an empty fallback is not looked up")

	_, err = f.FindWith(Tclsh, "/nowhere/tclsh")
	require.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, 3, calls, "a fallback that misses is not remembered either")
}

func TestMissesAreWordedForHowTheToolIsNamed(t *testing.T) {
	f := NewFinder(func(string) (string, error) { return "", errors.New("nope") })

	_, err := f.Find(Gh)
	require.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, "gh not found on PATH", err.Error(), "a bare name was searched for on PATH")

	_, err = f.FindWith(PortTclsh, "/opt/local/bin/port-tclsh")
	require.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, "port-tclsh not found on PATH", err.Error(), "the fallback does not change the sentence")

	_, err = f.Find(Tar)
	require.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, "/usr/bin/tar not found", err.Error(), "a pinned path was checked where it says")
}

func TestFindersDoNotShareAMemo(t *testing.T) {
	present := NewFinder(func(name string) (string, error) { return "/present/" + name, nil })
	absent := NewFinder(func(string) (string, error) { return "", errors.New("nope") })

	require.True(t, present.Have(Tart))
	assert.False(t, absent.Have(Tart), "one finder's success is not another's")
}

func TestNilLookupIsTheRealPATH(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "dockhand-tool-probe")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", dir)

	f := NewFinder(nil)
	path, err := f.Find(Tool("dockhand-tool-probe"))
	require.NoError(t, err)
	assert.Equal(t, bin, path)

	path, err = f.Find(Tool(bin))
	require.NoError(t, err)
	assert.Equal(t, bin, path, "a path is checked where it says")

	_, err = f.Find(Tool(filepath.Join(dir, "absent")))
	require.ErrorIs(t, err, ErrNotFound)
	_, err = f.Find(Tool("dockhand-tool-absent"))
	require.ErrorIs(t, err, ErrNotFound)
}

func TestConcurrentFindsLookUpOnce(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	f := NewFinder(func(name string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return "/fake/" + name, nil
	})

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			_, err := f.Find(Git)
			assert.NoError(t, err)
		})
	}
	wg.Wait()
	assert.Equal(t, 1, calls, "the lookup runs under the lock, so a race to the same tool is one lookup")
}

func TestZeroFinderIsUsable(t *testing.T) {
	// The type is exported, so a zero Finder is legal: its first hit
	// allocates the memo rather than writing into a nil map, and with
	// no lookup set it searches PATH.
	calls := 0
	f := &Finder{lookup: func(name string) (string, error) {
		calls++
		return "/fake/" + name, nil
	}}
	p, err := f.Find(Git)
	require.NoError(t, err)
	assert.Equal(t, "/fake/git", p)
	p, err = f.FindWith(Tart, "/fallback/tart")
	require.NoError(t, err)
	assert.Equal(t, "/fake/tart", p)
	_, err = f.Find(Git)
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "both hits remembered")

	var zero Finder
	p, err = zero.Find(Tool("sh"))
	require.NoError(t, err, "a zero Finder searches PATH")
	assert.NotEmpty(t, p)
}
