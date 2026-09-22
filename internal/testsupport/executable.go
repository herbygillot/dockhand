package testsupport

import (
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// WriteExecutable writes program to path for a test to run, creating the file
// with mode 0700 or truncating one that exists.
//
// The file is open for writing only while no process can be forked. A child
// forked meanwhile, for a parallel test's command, would inherit the write
// descriptor until it execs, and running the program then fails with ETXTBSY,
// "text file busy". Forks hold syscall.ForkLock for writing, so the read lock
// keeps every child from ever holding the descriptor.
func WriteExecutable(t testing.TB, path, program string) {
	t.Helper()
	syscall.ForkLock.RLock()
	err := os.WriteFile(path, []byte(program), 0700)
	syscall.ForkLock.RUnlock()
	require.NoError(t, err)
}
