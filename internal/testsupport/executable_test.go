package testsupport

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Without the fork lock, several of these hundred runs fail with ETXTBSY even
// on an idle machine.
func TestWrittenProgramsRunWhileOtherGoroutinesFork(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var forks sync.WaitGroup
	defer forks.Wait()
	defer cancel()
	for range 4 {
		forks.Go(func() {
			for ctx.Err() == nil {
				_ = exec.Command("/bin/sh", "-c", "exit 0").Run()
			}
		})
	}
	directory := t.TempDir()
	for i := range 100 {
		path := filepath.Join(directory, strconv.Itoa(i))
		WriteExecutable(t, path, "#!/bin/sh\nexit 0\n")
		require.NoError(t, exec.Command(path).Run())
	}
}
