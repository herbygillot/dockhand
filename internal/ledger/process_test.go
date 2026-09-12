package ledger_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/lock"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/stretchr/testify/require"
)

func TestWriterProcessesPreserveAcceptedUpdates(t *testing.T) {
	f := newFixture(t, "sha1")
	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	var commands []*exec.Cmd
	var starts []io.WriteCloser
	for i := 0; i < 3; i++ {
		command := exec.CommandContext(ctx, executable, "-test.run=^TestLedgerWriterProcess$")
		command.Env = append(isolatedGitEnv(), "DOCKHAND_LEDGER_TEST_ROOT="+f.repo.Root, fmt.Sprintf("DOCKHAND_LEDGER_TEST_WRITER=%d", i))
		command.Stderr = os.Stderr
		stdout, err := command.StdoutPipe()
		require.NoError(t, err)
		stdin, err := command.StdinPipe()
		require.NoError(t, err)
		require.NoError(t, command.Start())
		t.Cleanup(func() { stdin.Close(); command.Process.Kill(); command.Wait() })
		line, err := bufio.NewReader(stdout).ReadString('\n')
		require.NoError(t, err)
		require.Equal(t, "ready\n", line)
		commands = append(commands, command)
		starts = append(starts, stdin)
	}
	for _, stdin := range starts {
		_, err := io.WriteString(stdin, "go\n")
		require.NoError(t, err)
		require.NoError(t, stdin.Close())
	}
	for _, command := range commands {
		require.NoError(t, command.Wait())
	}
	snapshot := f.snapshot(t)
	require.Len(t, snapshot.State.Changes, 9)
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			require.Contains(t, snapshot.State.Changes, record.ChangeID(fmt.Sprintf("writer-%d-%d", i, j)))
		}
	}
}

func TestLedgerWriterProcess(t *testing.T) {
	root := os.Getenv("DOCKHAND_LEDGER_TEST_ROOT")
	if root == "" {
		return
	}
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	locks, err := lock.NewDirectory(filepath.Join(root, "locks"))
	require.NoError(t, err)
	writer, err := locks.File("repositories", repo.CommonDir, "ledger")
	require.NoError(t, err)
	store, err := ledger.New(repo, ledger.Options{WriterLock: writer})
	require.NoError(t, err)
	fmt.Println("ready")
	_, err = bufio.NewReader(os.Stdin).ReadString('\n')
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		id := record.ChangeID(fmt.Sprintf("writer-%s-%d", os.Getenv("DOCKHAND_LEDGER_TEST_WRITER"), i))
		require.NoError(t, store.Update(t.Context(), addChange(id)))
	}
}
