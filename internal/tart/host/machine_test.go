package host

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/tart"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func fixtureMachine(t *testing.T, script string) Machine {
	t.Helper()
	root := t.TempDir()
	executable := filepath.Join(root, "tart")
	testsupport.WriteExecutable(t, executable, "#!/bin/sh\nset -eu\n"+script)
	return Machine{Client: tart.Client{Executable: executable, Home: root}}
}

func TestCloneInheritsOperationAndImageLocks(t *testing.T) {
	m := fixtureMachine(t, `
case "$1" in
 list) printf '[]' ;;
 clone)
  [ -e /dev/fd/3 ] && [ -e /dev/fd/4 ]
  [ "$2" = base ] && [ "$3" = candidate ]
  [ "$TART_NO_AUTO_PRUNE" = 1 ]
  printf done > "$TART_HOME/cloned"
 ;;
esac
`)
	guard, err := os.Create(filepath.Join(m.Client.Home, "operation.lock"))
	require.NoError(t, err)
	defer guard.Close()
	m.Guard = guard
	require.NoError(t, m.Clone(t.Context(), "base", "candidate"))
	require.FileExists(t, filepath.Join(m.Client.Home, "cloned"))
}

func TestCloneRefusesExistingAndIncompleteDestinations(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		t.Run(map[bool]string{false: "listed", true: "incomplete"}[incomplete], func(t *testing.T) {
			m := fixtureMachine(t, `
case "$1" in
 list) cat "$TART_HOME/list.json" ;;
 clone) touch "$TART_HOME/overwritten" ;;
esac
`)
			list := `[{"Name":"candidate","Source":"local","State":"stopped"}]`
			if incomplete {
				list = `[]`
				require.NoError(t, os.MkdirAll(filepath.Join(m.Client.Home, "vms", "candidate"), 0700))
			}
			require.NoError(t, os.WriteFile(filepath.Join(m.Client.Home, "list.json"), []byte(list), 0600))
			require.Error(t, m.Clone(t.Context(), "base", "candidate"))
			require.NoFileExists(t, filepath.Join(m.Client.Home, "overwritten"))
		})
	}
}

func TestDeleteRequiresStoppedImageAndConfirmedAbsence(t *testing.T) {
	for _, mode := range []string{"running", "retained", "removed", "absent"} {
		t.Run(mode, func(t *testing.T) {
			m := fixtureMachine(t, `
case "$1" in
 list) cat "$TART_HOME/list.json" ;;
 delete)
  touch "$TART_HOME/deleted"
  if [ -f "$TART_HOME/remove" ]; then printf '[]' > "$TART_HOME/list.json"; fi
 ;;
esac
`)
			list := `[{"Name":"candidate","Source":"local","State":"stopped"}]`
			if mode == "running" {
				list = `[{"Name":"candidate","Source":"local","State":"running"}]`
			}
			if mode == "absent" {
				list = `[]`
			}
			if mode == "removed" {
				require.NoError(t, os.WriteFile(filepath.Join(m.Client.Home, "remove"), nil, 0600))
			}
			require.NoError(t, os.WriteFile(filepath.Join(m.Client.Home, "list.json"), []byte(list), 0600))
			err := m.Delete(t.Context(), "candidate")
			switch mode {
			case "running":
				require.ErrorContains(t, err, "running")
				require.NoFileExists(t, filepath.Join(m.Client.Home, "deleted"))
			case "retained":
				require.ErrorContains(t, err, "not confirmed")
			case "removed":
				require.NoError(t, err)
				require.FileExists(t, filepath.Join(m.Client.Home, "deleted"))
			case "absent":
				require.NoError(t, err)
				require.NoFileExists(t, filepath.Join(m.Client.Home, "deleted"))
			}
		})
	}
}

func TestForegroundReportsEarlyFailureWithDiagnostics(t *testing.T) {
	m := fixtureMachine(t, `
[ "$1" = run ] && [ "$2" = --no-graphics ] && [ "$5" = candidate ]
[ "$TART_NO_AUTO_PRUNE" = 1 ] && [ "$LC_ALL" = C ]
printf 'fixture boot failure' >&2
exit 42
`)
	done, err := m.StartForeground("candidate")
	require.NoError(t, err)
	select {
	case err := <-done:
		require.ErrorContains(t, err, "fixture boot failure")
		require.ErrorContains(t, err, "exit status 42")
	case <-t.Context().Done():
		t.Fatal("process failed to exit")
	}
	m.Client.Executable = filepath.Join(m.Client.Home, "absent")
	_, err = m.StartForeground("candidate")
	require.Error(t, err)
}
