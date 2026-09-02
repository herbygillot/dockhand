//go:build darwin || linux

package tool

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// open opens a path read-write for the life of the test.
func open(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, f.Close()) })
	return f
}

func TestIsTerminalSaysNoToEverythingThatIsNotOne(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, r.Close())
		require.NoError(t, w.Close())
	})
	plain, err := os.CreateTemp(t.TempDir(), "plain")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, plain.Close()) })

	cases := []struct {
		name string
		fd   uintptr
	}{
		// The case that motivates the kernel question: a character
		// device, and the thing a redirected stdin usually is.
		{"dev null", open(t, "/dev/null").Fd()},
		{"the read end of a pipe", r.Fd()},
		{"the write end of a pipe", w.Fd()},
		{"a regular file", plain.Fd()},
		{"a descriptor that is not open", ^uintptr(0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.False(t, IsTerminal(c.fd))
		})
	}
}

func TestIsTerminalSaysYesToAPTY(t *testing.T) {
	// openPTY hands out the terminal side of a fresh pseudo-terminal, or
	// skips when the machine will not allocate one.
	tty := openPTY(t)
	assert.True(t, IsTerminal(tty.Fd()))
}
