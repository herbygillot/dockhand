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
	return Machine{Client: tart.Client{Env: os.Environ(), Executable: executable, Home: root}}
}

// Clone hands tart the image lock it holds, so the image is read-locked
// for as long as tart clones it.
func TestCloneInheritsTheImageLock(t *testing.T) {
	t.Parallel()
	m := fixtureMachine(t, `
case "$1" in
 list) printf '[]' ;;
 clone)
  [ -e /dev/fd/3 ]
  [ "$2" = base ] && [ "$3" = candidate ]
  [ "$TART_NO_AUTO_PRUNE" = 1 ]
  printf done > "$TART_HOME/cloned"
 ;;
esac
`)
	require.NoError(t, m.Clone(t.Context(), "base", "candidate"))
	require.FileExists(t, filepath.Join(m.Client.Home, "cloned"))
}

// Clone refuses a name Tart lists, since `tart clone` onto a stopped VM
// replaces it without a word, and reads nothing inside the Tart home: a
// directory Tart does not list is Tart's to clean up.
func TestCloneRefusesListedDestinationsOnly(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// A delete of a VM the listing shows stopped is Tart's answer, since 2.40.0
// says when it left a VM (openai/tart#1345, #38): one Tart says is gone is
// deleted, and a refusal is said as Tart says it; a running VM is refused
// before Tart is asked; and the removal is confirmed by a listing after.
func TestADeleteIsTartsAnswer(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"deleted", "gone", "refused", "running"} {
		t.Run(mode, func(t *testing.T) {
			m := fixtureMachine(t, `
case "$1" in
 list) if [ -f "$TART_HOME/deleted" ]; then printf '[]'; else cat "$TART_HOME/list.json"; fi ;;
 delete)
  if [ -f "$TART_HOME/refuse" ]; then echo "failed to delete VM \"$2\": it is running" >&2; exit 1; fi
  touch "$TART_HOME/deleted"
  if [ -f "$TART_HOME/gone" ]; then echo "the specified VM \"$2\" does not exist" >&2; exit 2; fi
 ;;
esac
`)
			list := `[{"Name":"candidate","Source":"local","State":"stopped"}]`
			if mode == "running" {
				list = `[{"Name":"candidate","Source":"local","State":"running"}]`
			}
			require.NoError(t, os.WriteFile(filepath.Join(m.Client.Home, "list.json"), []byte(list), 0600))
			if mode == "gone" || mode == "refused" {
				require.NoError(t, os.WriteFile(filepath.Join(m.Client.Home, map[string]string{"gone": "gone", "refused": "refuse"}[mode]), nil, 0600))
			}
			err := m.Delete(t.Context(), "candidate")
			switch mode {
			case "refused":
				require.ErrorContains(t, err, "it is running")
			case "running":
				require.ErrorContains(t, err, "cannot delete running VM candidate")
				require.NoFileExists(t, filepath.Join(m.Client.Home, "deleted"), "Tart wasn't asked")
			default:
				require.NoError(t, err)
			}
		})
	}
}

func TestForegroundReportsEarlyFailureWithDiagnostics(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// DeleteCached removes only an image Tart lists as cached from a registry,
// and says so where one it deleted is still listed; a local VM of the
// same name, or none, is left alone (the test plan's step 2, item 19).
func TestDeleteCachedRemovesOnlyARegistryImage(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"cached", "retained", "local", "absent"} {
		t.Run(mode, func(t *testing.T) {
			m := fixtureMachine(t, `
case "$1" in
 list) cat "$TART_HOME/list.json" ;;
 delete)
  touch "$TART_HOME/deleted"
  if [ ! -f "$TART_HOME/retain" ]; then printf '[]' > "$TART_HOME/list.json"; fi
 ;;
esac
`)
			list := `[{"Name":"ghcr.io/cirruslabs/macos-tahoe-vanilla:latest","Source":"OCI","State":"stopped"}]`
			switch mode {
			case "retained":
				require.NoError(t, os.WriteFile(filepath.Join(m.Client.Home, "retain"), nil, 0600))
			case "local":
				list = `[{"Name":"ghcr.io/cirruslabs/macos-tahoe-vanilla:latest","Source":"local","State":"stopped"}]`
			case "absent":
				list = `[]`
			}
			require.NoError(t, os.WriteFile(filepath.Join(m.Client.Home, "list.json"), []byte(list), 0600))
			err := m.DeleteCached(t.Context(), "ghcr.io/cirruslabs/macos-tahoe-vanilla:latest")
			switch mode {
			case "cached":
				require.NoError(t, err)
				require.FileExists(t, filepath.Join(m.Client.Home, "deleted"))
			case "retained":
				require.ErrorContains(t, err, "cached image ghcr.io/cirruslabs/macos-tahoe-vanilla:latest is still listed after delete")
			case "local", "absent":
				require.NoError(t, err)
				require.NoFileExists(t, filepath.Join(m.Client.Home, "deleted"), "nothing cached to delete")
			}
		})
	}
}

// A delete is Tart's answer, confirmed by a listing after it, as the
// design keeps: a VM still listed after a delete that said it worked is
// said (Codex's review of #193 steps 5 and 6, finding 7).
func TestADeleteIsConfirmedByAListing(t *testing.T) {
	t.Parallel()
	m := fixtureMachine(t, `
case "$1" in
 delete) exit 0 ;;
 list) printf '[{"Name":"candidate","Source":"local","State":"stopped"}]' ;;
esac
`)
	require.ErrorContains(t, m.Delete(t.Context(), "candidate"), "still listed after delete")
}
