package local

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/guest"
	"github.com/herbygillot/dockhand/internal/filelock"
)

// One check owns a prefix from what it puts back first to its own
// put-back: a second, from another provider, as another database's would
// be, waits, saying whose check it waits for, and enters only once the
// first has let go (Codex's review of 8e314b96, finding 1, its probe).
func TestAPrefixIsOwnedForAWholeCheck(t *testing.T) {
	prefix, env, _ := fakePrefix(t)
	release := filepath.Join(t.TempDir(), "release")
	entries := filepath.Join(t.TempDir(), "entries")
	temp := t.TempDir()
	script := fmt.Sprintf(`set root $::env(DOCKHAND_GUEST_ROOT)
set fd [open $root/results.json w]
puts $fd {{"protocol":1,"state":"running","environment":{"snapshot":"7"},"targets":[]}}
close $fd
set marker [open $root/entered w]; close $marker
set log [open {%[2]s} a]; puts $log entered; close $log
while {![file exists {%[1]s}]} { after 10 }
set fd [open $root/results.json w]
puts $fd {{"protocol":1,"state":"finished","environment":{"snapshot":"7","restored":"yes"},"targets":[]}}
close $fd
`, release, entries)
	provider := func() *Local {
		l := localProvider(t, env)
		l.Temp = temp
		l.stager = func(_ context.Context, _ buildenv.Job, input guest.Input, dir string) error {
			data, err := json.Marshal(input)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, "input.json"), data, 0o644); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, "guest.tcl"), []byte(script), 0o644)
		}
		return l
	}
	entered := func() int {
		dirs, _ := filepath.Glob(filepath.Join(temp, localDirectory+"*", "entered"))
		return len(dirs)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	completed := make(chan error, 2)
	go func() { completed <- provider().Execute(ctx, localJob(t, prefix), &fakeBuild{}) }()
	require.Eventually(t, func() bool { return entered() == 1 }, 30*time.Second, 10*time.Millisecond)
	waiting := &fakeBuild{}
	go func() { completed <- provider().Execute(ctx, localJob(t, prefix), waiting) }()
	require.Eventually(t, func() bool {
		waiting.mu.Lock()
		defer waiting.mu.Unlock()
		return strings.Contains(strings.Join(waiting.progress, "\n"), "waiting for "+prefix+", held by check-3 (pid ")
	}, 30*time.Second, 10*time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, 1, entered(), "the second check doesn't begin while the first owns the prefix")
	// status names who holds it, as the wait does.
	changes, err := provider().Changes(ctx)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	require.True(t, changes[0].Building)
	require.Equal(t, fmt.Sprintf("check-3 (pid %d)", os.Getpid()), changes[0].Holder)

	require.NoError(t, os.WriteFile(release, nil, 0o644))
	require.NoError(t, <-completed)
	require.NoError(t, <-completed)
	log, err := os.ReadFile(entries)
	require.NoError(t, err)
	require.Equal(t, "entered\nentered\n", string(log), "the second runs once the first let go")
}

// A holder whose process ended is said as one, so a wait on a lock a
// crashed process left isn't taken for a live check's.
func TestADeadHoldersLockIsSaidAsEnded(t *testing.T) {
	t.Parallel()
	path := filelock.Path(t.TempDir(), "prefix:/opt/local")
	record, err := json.Marshal(holder{PID: 999999, Start: "darwin:1.0", Check: "check-9", Prefix: "/opt/local"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(holderPath(path, 502), record, 0o644))
	require.Equal(t, "a check that ended, check-9 (pid 999999)", held(path))
	require.Equal(t, "another check", held(path+".none"))
}

// A check waits for a guest program its crashed dockhand left building in
// the prefix, where it snapshotted and built beside it: the lock died with
// the dockhand, the program, under sudo in a group of its own, didn't (the
// architecture thread's finding on rc7). Once the program ends, its
// directory is an ordinary leftover.
func TestACheckWaitsForAGuestItsCrashedDockhandLeft(t *testing.T) {
	prefix, env, _ := fakePrefix(t)
	temp := t.TempDir()
	child := exec.Command("/bin/sleep", "60")
	require.NoError(t, child.Start())
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	left := filepath.Join(temp, localDirectory+"crashed")
	require.NoError(t, os.MkdirAll(left, 0o755))
	owned, err := json.Marshal(owner{PID: 999999, Start: "darwin:1.0", Check: "check-12", Prefix: prefix})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(left, "owner.json"), owned, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(left, "results.json"), fmt.Appendf(nil, `{"protocol":1,"state":"running","environment":{"pid":"%d"},"targets":[]}`, child.Process.Pid), 0o644))

	entries := filepath.Join(t.TempDir(), "entries")
	l := localProvider(t, env)
	l.Temp = temp
	l.stager = func(_ context.Context, _ buildenv.Job, input guest.Input, dir string) error {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "input.json"), data, 0o644); err != nil {
			return err
		}
		script := fmt.Sprintf(`set log [open {%s} a]; puts $log entered; close $log
set fd [open $::env(DOCKHAND_GUEST_ROOT)/results.json w]
puts $fd {{"protocol":1,"state":"finished","environment":{},"targets":[]}}
close $fd
`, entries)
		return os.WriteFile(filepath.Join(dir, "guest.tcl"), []byte(script), 0o644)
	}
	waiting := &fakeBuild{}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- l.Execute(ctx, localJob(t, prefix), waiting) }()
	require.Eventually(t, func() bool {
		waiting.mu.Lock()
		defer waiting.mu.Unlock()
		return strings.Contains(strings.Join(waiting.progress, "\n"), fmt.Sprintf("waiting for check-12's program (pid %d), which its dockhand left running, to finish in %s", child.Process.Pid, prefix))
	}, 5*time.Second, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	require.NoFileExists(t, entries, "no snapshot while the orphan builds")

	require.NoError(t, child.Process.Kill())
	_ = child.Wait()
	require.NoError(t, <-done)
	require.FileExists(t, entries, "the check ran once the orphan ended")
	require.NoDirExists(t, left, "the orphan's directory, which changed nothing, is removed")
}
