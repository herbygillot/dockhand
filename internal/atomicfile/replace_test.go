package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateWritesThroughACallbackAndLeavesFailuresUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "file")
	require.NoError(t, Create(path, 0640, func(file *os.File) error {
		_, err := file.WriteString("first")
		return err
	}))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "first", string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0640), info.Mode().Perm())
	boom := errors.New("boom")
	require.ErrorIs(t, Create(path, 0640, func(file *os.File) error {
		_, _ = file.WriteString("partial")
		return boom
	}), boom)
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "first", string(data), "a failing callback leaves the destination untouched")
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temporary file is left behind")
}

func TestReplaceDirectoryRetiresThePreviousDirectoryAfterTheReplacementExists(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "generation")
	build := func(marker string) func(string) error {
		return func(temp string) error { return os.WriteFile(filepath.Join(temp, "marker"), []byte(marker), 0600) }
	}
	require.NoError(t, ReplaceDirectory(destination, build("one")))
	data, err := os.ReadFile(filepath.Join(destination, "marker"))
	require.NoError(t, err)
	require.Equal(t, "one", string(data))
	require.NoError(t, ReplaceDirectory(destination, build("two")))
	data, err = os.ReadFile(filepath.Join(destination, "marker"))
	require.NoError(t, err)
	require.Equal(t, "two", string(data))
	boom := errors.New("boom")
	require.ErrorIs(t, ReplaceDirectory(destination, func(temp string) error {
		_ = os.WriteFile(filepath.Join(temp, "marker"), []byte("three"), 0600)
		return boom
	}), boom)
	data, err = os.ReadFile(filepath.Join(destination, "marker"))
	require.NoError(t, err)
	require.Equal(t, "two", string(data), "a failing build leaves the previous directory")
	entries, err := os.ReadDir(filepath.Dir(destination))
	require.NoError(t, err)
	require.Len(t, entries, 1, "temporary and retired directories are removed")
}
