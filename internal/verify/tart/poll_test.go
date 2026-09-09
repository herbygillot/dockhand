package tart

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
)

// fakeTart stands a script in for the tart CLI: it lists one VM in the
// state given, and refuses every exec — which is what a guest that is
// not answering looks like from out here.
func fakeTart(t *testing.T, vm, state string) *tool.Finder {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tart")
	script := fmt.Sprintf(`#!/bin/sh
case "$*" in
  *"--format json"*)
    printf '[{"Name":"%[1]s","State":"%[2]s","Source":"local"}]\n' ;;
  "list --source local")
    printf 'Source Name Disk Size Accessed State\nlocal %[1]s 100 GB 33 GB 1 hour ago %[2]s\n' ;;
  *)
    echo "no route to guest" >&2; exit 1 ;;
esac
`, vm, state)
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755)) //nolint:gosec // a test fixture
	return tool.NewFinder(func(string) (string, error) { return path, nil })
}

// A GUEST STILL BOOTING HAS NOT REPORTED, AND MAY YET. Poll must not
// invent an outcome for it.
func TestPollReadsAnUnreachableRunningGuestAsStillRunning(t *testing.T) {
	p := Provider{Tools: fakeTart(t, "w-1", "running")}
	got, err := p.Poll(context.Background(), verify.Job{Provider: "tart", ID: "w-1"})
	require.NoError(t, err)
	assert.Equal(t, verify.Running, got.State)
}

// A GUEST THAT HAS STOPPED WILL NEVER REPORT, AND SAYING "RUNNING" IS A
// WAIT THAT CANNOT END.
//
// Both were one answer here: Exec fails, and the reasoning — "not
// answering yet, or no longer is" — treated the two as one fact. They
// are not. Measured on a real cohort: the guest died partway through a
// Skia compile, the VM stayed in the listing as `stopped`, and Poll
// answered Running to every poll a `--wait 300m` watcher made. That is
// rule 7's shape, "I could not reach it" said as "it is working".
func TestPollReadsAStoppedGuestAsTerminalRatherThanRunningForever(t *testing.T) {
	p := Provider{Tools: fakeTart(t, "w-1", "stopped")}
	got, err := p.Poll(context.Background(), verify.Job{Provider: "tart", ID: "w-1"})
	require.NoError(t, err)
	assert.Equal(t, verify.Errored, got.State)
	assert.Contains(t, got.Detail, "stopped before the run reported an outcome")
}

// answeringTart stands in for a guest that is UP and TALKING: it lists
// itself as running and answers every exec with the given text, which
// is how a healthy environment holding a broken runner looks from here.
func answeringTart(t *testing.T, vm, say string) *tool.Finder {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tart")
	script := fmt.Sprintf(`#!/bin/sh
case "$*" in
  *"--format json"*)
    printf '[{"Name":"%[1]s","State":"running","Source":"local"}]\n' ;;
  "list --source local")
    printf 'Source Name Disk Size Accessed State\nlocal %[1]s 100 GB 33 GB 1 hour ago running\n' ;;
  *)
    printf '%%s' '%[2]s' ;;
esac
`, vm, say)
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755)) //nolint:gosec // a test fixture
	return tool.NewFinder(func(string) (string, error) { return path, nil })
}

// A GUEST THAT ANSWERS IS NOT A BROKEN MACHINE. This one is running,
// reachable, and replies to the exec — and what it replies with is not
// one of the three words dockhand's own runner writes. Every part of
// the environment did its job; the apparatus dockhand put inside it did
// not, and that is Faulted.
//
// It was Errored, which is the sentence "the environment could not
// answer" said about an environment that just answered — and it sent
// the person to go and fix a machine that was working.
func TestPollReadsAHealthyGuestWithNoRunnerStateAsFaulted(t *testing.T) {
	p := Provider{Tools: answeringTart(t, "w-1", "")}
	got, err := p.Poll(context.Background(), verify.Job{Provider: "tart", ID: "w-1"})
	require.NoError(t, err)
	assert.Equal(t, verify.Faulted, got.State)
	assert.Contains(t, got.Detail, "the runner did not start")
}

// THE THREE WORDS THE RUNNER DOES WRITE still mean what they meant: a
// fault is what is left over, never a reading the runner intended.
func TestPollStillReadsTheRunnersOwnWords(t *testing.T) {
	for word, want := range map[string]verify.State{
		"passed":  verify.Passed,
		"failed":  verify.Failed,
		"running": verify.Running,
	} {
		p := Provider{Tools: answeringTart(t, "w-1", word)}
		got, err := p.Poll(context.Background(), verify.Job{Provider: "tart", ID: "w-1"})
		require.NoError(t, err)
		assert.Equal(t, want, got.State, "runner said %q", word)
	}
}

// Running IS ABOUT THE VM AND NOT ABOUT THE EXEC, and a VM that is not
// there at all is not running — without an error, because absence is
// HasVM's question.
func TestRunningTellsRunningFromStoppedFromAbsent(t *testing.T) {
	ctx := context.Background()

	up, err := Running(ctx, fakeTart(t, "w-1", "running"), "w-1")
	require.NoError(t, err)
	assert.True(t, up)

	down, err := Running(ctx, fakeTart(t, "w-1", "stopped"), "w-1")
	require.NoError(t, err)
	assert.False(t, down)

	gone, err := Running(ctx, fakeTart(t, "w-1", "running"), "somebody-else")
	require.NoError(t, err)
	assert.False(t, gone)
}
