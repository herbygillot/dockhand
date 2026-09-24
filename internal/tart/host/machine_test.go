package host

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

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

// Clone refuses a name Tart lists, since `tart clone` onto a stopped VM
// replaces it without a word, and reads nothing inside the Tart home: a
// directory Tart does not list is Tart's to clean up.
func TestCloneRefusesListedDestinationsOnly(t *testing.T) {
	for _, listed := range []bool{true, false} {
		t.Run(map[bool]string{true: "listed", false: "unlisted directory"}[listed], func(t *testing.T) {
			m := fixtureMachine(t, `
case "$1" in
 list) cat "$TART_HOME/list.json" ;;
 clone) touch "$TART_HOME/overwritten" ;;
esac
`)
			list := `[{"Name":"candidate","Source":"local","State":"stopped"}]`
			if !listed {
				list = `[]`
				require.NoError(t, os.MkdirAll(filepath.Join(m.Client.Home, "vms", "candidate"), 0700))
			}
			require.NoError(t, os.WriteFile(filepath.Join(m.Client.Home, "list.json"), []byte(list), 0600))
			err := m.Clone(t.Context(), "base", "candidate")
			if listed {
				require.ErrorContains(t, err, "refusing to overwrite existing VM candidate")
				require.NoFileExists(t, filepath.Join(m.Client.Home, "overwritten"))
				return
			}
			require.NoError(t, err)
			require.FileExists(t, filepath.Join(m.Client.Home, "overwritten"))
		})
	}
}

// Rename moves only a stopped VM, and only to a name Tart does not list:
// Tart renames running VMs, and refuses a listed target only by chance of
// its own checks.
func TestRenameRequiresAStoppedSourceAndAnUnlistedTarget(t *testing.T) {
	for _, test := range []struct{ name, list, want string }{
		{"renamed", `[{"Name":"from","Source":"local","State":"stopped"}]`, ""},
		{"running", `[{"Name":"from","Source":"local","State":"running"}]`, "cannot rename running VM from"},
		{"taken", `[{"Name":"from","Source":"local","State":"stopped"},{"Name":"to","Source":"local","State":"stopped"}]`, "refusing to overwrite existing VM to"},
		{"missing", `[]`, "does not exist"},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := fixtureMachine(t, `
case "$1" in
 list) cat "$TART_HOME/list.json" ;;
 rename) [ "$2" = from ] && [ "$3" = to ]; touch "$TART_HOME/renamed" ;;
esac
`)
			require.NoError(t, os.WriteFile(filepath.Join(m.Client.Home, "list.json"), []byte(test.list), 0600))
			err := m.Rename(t.Context(), "from", "to")
			if test.want == "" {
				require.NoError(t, err)
				require.FileExists(t, filepath.Join(m.Client.Home, "renamed"))
				return
			}
			require.ErrorContains(t, err, test.want)
			require.NoFileExists(t, filepath.Join(m.Client.Home, "renamed"))
		})
	}
}

// A delete is confirmed by the VM's absence from the listing, never by what
// `tart delete` says: it reports a running VM as one that "does not exist"
// and leaves it (openai/tart#1345), and a failed delete can have worked.
func TestDeleteRequiresStoppedImageAndConfirmedAbsence(t *testing.T) {
	for _, mode := range []string{"running", "retained", "collision", "removed", "removed despite error", "absent"} {
		t.Run(mode, func(t *testing.T) {
			m := fixtureMachine(t, `
case "$1" in
 list) cat "$TART_HOME/list.json" ;;
 delete)
  touch "$TART_HOME/deleted"
  if [ -f "$TART_HOME/remove" ]; then printf '[]' > "$TART_HOME/list.json"; fi
  if [ -f "$TART_HOME/fail" ]; then echo "the specified VM \"$2\" does not exist" >&2; exit 2; fi
 ;;
esac
`)
			if mode == "collision" || mode == "removed despite error" {
				require.NoError(t, os.WriteFile(filepath.Join(m.Client.Home, "fail"), nil, 0600))
			}
			list := `[{"Name":"candidate","Source":"local","State":"stopped"}]`
			if mode == "running" {
				list = `[{"Name":"candidate","Source":"local","State":"running"}]`
			}
			if mode == "absent" {
				list = `[]`
			}
			if mode == "removed" || mode == "removed despite error" {
				require.NoError(t, os.WriteFile(filepath.Join(m.Client.Home, "remove"), nil, 0600))
			}
			require.NoError(t, os.WriteFile(filepath.Join(m.Client.Home, "list.json"), []byte(list), 0600))
			err := m.Delete(t.Context(), "candidate")
			switch mode {
			case "running":
				require.ErrorContains(t, err, "running")
				require.NoFileExists(t, filepath.Join(m.Client.Home, "deleted"))
			case "retained":
				require.ErrorContains(t, err, "still listed after delete")
			case "collision":
				require.ErrorContains(t, err, "still listed after delete")
				require.ErrorContains(t, err, "does not exist")
				require.NotErrorIs(t, err, tart.ErrVMMissing)
			case "removed", "removed despite error":
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
	run, err := m.StartForeground("candidate")
	require.NoError(t, err)
	select {
	case <-run.Done():
		require.ErrorContains(t, run.Err(), "fixture boot failure")
		require.ErrorContains(t, run.Err(), "exit status 42")
	case <-t.Context().Done():
		t.Fatal("process failed to exit")
	}
	m.Client.Executable = filepath.Join(m.Client.Home, "absent")
	_, err = m.StartForeground("candidate")
	require.Error(t, err)
}

// Setup's `tart run` has a process group of its own, so the terminal's
// Ctrl-C reaches dockhand, and dockhand stops it: `tart stop` first, then
// SIGINT, then SIGKILL, whatever `tart stop` says.
func TestForegroundRunsInItsOwnGroupAndStopsByEscalation(t *testing.T) {
	for _, test := range []struct{ name, script string }{
		{"tart stop", `
case "$1" in
 run) while [ ! -f "$TART_HOME/stopped" ]; do sleep 0.05; done ;;
 stop) touch "$TART_HOME/stopped" ;;
esac
`},
		{"SIGINT", `
case "$1" in
 run) trap 'exit 0' INT; while :; do sleep 0.05; done ;;
 stop) echo 'VM "candidate" is not running' >&2; exit 2 ;;
esac
`},
		{"SIGKILL", `
case "$1" in
 run) trap '' INT; while :; do sleep 0.05; done ;;
 stop) echo 'fixture stop failure' >&2; exit 1 ;;
esac
`},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := fixtureMachine(t, test.script)
			run, err := m.StartForeground("candidate")
			require.NoError(t, err)
			group, err := syscall.Getpgid(run.process.Pid)
			require.NoError(t, err)
			require.Equal(t, run.process.Pid, group, "the run leads its own process group")
			require.NotEqual(t, syscall.Getpgrp(), group)
			require.NoError(t, run.Stop(t.Context(), 300*time.Millisecond))
			select {
			case <-run.Done():
			default:
				t.Fatal("the run is still going after Stop")
			}
		})
	}
}
