package local

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/buildenv/guest"
	"github.com/herbygillot/dockhand/internal/buildenv/guest/guesttest"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// asIs is two targets checked in a prefix of the person's: among
// whatever is active, put back after.
func asIs(local guest.Local) guest.Input {
	local.Note = "dockhand check-1 attempt 1"
	return guest.Input{Tests: "declared", Local: &local, Targets: []guest.Target{
		{ID: "libharbor", Name: "libharbor", Portfile: "devel/libharbor/Portfile", Log: buildenv.LogName("libharbor")},
		{ID: "harbor-cli", Name: "harbor-cli", Portfile: "devel/harbor-cli/Portfile", DependsOn: []string{"libharbor"}, Log: buildenv.LogName("harbor-cli")},
	}}
}

// registry is the fake port's registry of active ports, as ACTIVEFILE
// has it, holding these lines.
func registry(t *testing.T, lines ...string) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "active")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "")), 0o644))
	return path, "ACTIVEFILE=" + path
}

func readRegistry(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// fakeBuild records what the provider records.
type fakeBuild struct {
	mu           sync.Mutex
	results      []model.TargetResult
	progress     []string
	observed     []model.Observed
	refs         []string
	dependencies []string
}

func (b *fakeBuild) Refer(ref string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refs = append(b.refs, ref)
	return nil
}
func (b *fakeBuild) Observe(observed model.Observed) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.observed = append(b.observed, observed)
	return nil
}
func (b *fakeBuild) Keep(_ model.TargetID, name string, fetch func(path string) error) error {
	directory, err := os.MkdirTemp("", "dockhand-kept-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	return fetch(filepath.Join(directory, name))
}
func (b *fakeBuild) KeepDependency(port model.ActivePort, _ string, _ func(path string) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dependencies = append(b.dependencies, port.Name)
	return nil
}
func (b *fakeBuild) Blocked(model.TargetID) (model.TargetID, bool) { return "", false }
func (b *fakeBuild) Record(report buildenv.TargetReport) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.results = append(b.results, report.Result)
	return nil
}
func (b *fakeBuild) Progress(message string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.progress = append(b.progress, message)
}
func (b *fakeBuild) Waiting(message string) { b.Progress(message) }
func (b *fakeBuild) Holding(string) string  { return "" }
func (b *fakeBuild) Canceled() bool         { return false }

var tahoe = model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}

// fakePrefix is a MacPorts prefix of the person's for the local provider:
// the fake port, and a port-tclsh that runs the guest program under the
// test's MacPorts tclsh with MacPorts itself stood in for, as guestRun
// does. env are the fake port's settings; ACTIVEFILE is its registry.
func fakePrefix(t *testing.T, env ...string) (prefix string, environment []string, portLog string) {
	t.Helper()
	root := t.TempDir()
	prefix = filepath.Join(root, "prefix")
	require.NoError(t, os.MkdirAll(filepath.Join(prefix, "bin"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(prefix, "etc", "macports"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(prefix, "var", "macports", "registry"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(prefix, "etc", "macports", "sources.conf"), []byte("rsync://rsync.macports.org/macports/release/tarballs/ports.tar [default]\n"), 0o644))
	testsupport.WriteExecutable(t, filepath.Join(prefix, "bin", "port"), guesttest.FakePort)
	prelude := filepath.Join(root, "prelude.tcl")
	require.NoError(t, os.WriteFile(prelude, []byte(`
package provide macports 1.0
namespace eval macports {variable os_platform darwin; variable os_major 25; variable build_arch arm64}
proc mportinit {} {}
proc declares_tests {portdir name variants} { return 0 }
proc checkout {portdir name variants} { return "" }
proc work_dir {portdir name variants} { return "" }
proc path_dependencies {portdir name variants} { return {} }
set foreignManagers {}
set argv0 [lindex $argv 0]
set argv [lrange $argv 1 end]
source $argv0
`), 0o644))
	tclsh := testsupport.MacPortsTclsh(t)
	testsupport.WriteExecutable(t, filepath.Join(prefix, "bin", "port-tclsh"), "#!/bin/sh\nexec "+tclsh+" "+prelude+" \"$@\"\n")
	otool := filepath.Join(root, "otool")
	testsupport.WriteExecutable(t, otool, guesttest.FakeOtool)
	portLog = filepath.Join(root, "port.log")
	environment = append(append(os.Environ(), "DOCKHAND_GUEST_OTOOL="+otool, "CONTENTS=", "PROVIDES=", "PORT_LOG="+portLog, "TESTED=", "DEPS=", "RDEPS=", "FAIL=",
		"ACTIVE=", "ARCHIVES="+root, "UNRESOLVED=", "DEPLINES=", "LINTSAYS=", "QUIET=", "HANG=", "NOARCHIVE=", "DEPFAILS=", "INSTALLED=", "DEPVERSION=", "CHECKOUTS="+filepath.Join(root, "checkouts")), env...)
	return prefix, environment, portLog
}

// localProvider builds in a fake prefix, its checks' directories in a
// temporary folder, staging only the guest program, its input, and an
// empty tree.
func localProvider(t *testing.T, env []string) *Local {
	return &Local{Poll: 10 * time.Millisecond, Temp: t.TempDir(), Env: env,
		stager: func(_ context.Context, _ buildenv.Job, input guest.Input, dir string) error {
			data, err := json.Marshal(input)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Join(dir, "ports"), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, "ports", "PortIndex"), nil, 0o644); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, "guest.tcl"), guest.Program, 0o644); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, "input.json"), data, 0o644)
		}}
}

func localJob(t *testing.T, prefix string) buildenv.Job {
	return buildenv.Job{
		Run:         model.Run{ID: "run_7", Number: 3},
		Execution:   model.GuestExecution{ID: "ex_1", Attempt: 1},
		Plan:        model.Plan{Tests: model.TestsDeclared},
		Environment: model.Environment{Provider: buildenv.Local, Platform: tahoe, Prefix: prefix},
		Directory:   t.TempDir(),
		Targets: []buildenv.Target{
			{PlanTarget: model.PlanTarget{ID: "libharbor", Target: model.Target{Name: "libharbor", Portfile: "devel/libharbor/Portfile"}}},
			{PlanTarget: model.PlanTarget{ID: "harbor-cli", Target: model.Target{Name: "harbor", Subport: "harbor-cli", Portfile: "devel/harbor/Portfile"}}, DependsOn: []model.TargetID{"libharbor"}},
		},
	}
}

// The local provider runs the guest program in the prefix, as the person
// where they can write its registry, with MacPorts reading the staged tree
// first through the check's own macports.conf, the prefix's sources after
// it with their default. Each target is recorded as in a clone, and what
// the prefix held is observed beside it; the prefix is put back after,
// and the check's directory removed.
func TestTheLocalProviderBuildsInThePrefixAndPutsItBack(t *testing.T) {
	t.Parallel()
	path, active := registry(t, "  zlib @1.3.2_0 (active)\n")
	prefix, env, portLog := fakePrefix(t, active, "DEPS=libharbor=zlib harbor-cli=libharbor")
	l := localProvider(t, env)
	job := localJob(t, prefix)
	build := &fakeBuild{}
	require.NoError(t, l.Execute(t.Context(), job, build))
	require.Len(t, build.results, 2)
	require.Equal(t, model.OutcomePassed, build.results[0].Outcome)
	require.Equal(t, model.OutcomePassed, build.results[1].Outcome)
	require.Equal(t, 1, build.observed[0].ActivePorts)
	require.Len(t, build.refs, 1)
	require.Contains(t, build.progress, "building in "+prefix+" (as you)")
	require.NoDirExists(t, build.refs[0], "the check's directory goes once the prefix is put back")
	require.Equal(t, "  zlib @1.3.2_0 (active)\n", readRegistry(t, path))
	conf, err := os.ReadFile(portLog + ".conf")
	require.NoError(t, err)
	require.Equal(t, "sources_conf "+filepath.Join(build.refs[0], "sources.conf")+"\nfile://"+filepath.Join(build.refs[0], "ports")+" [nosync]\nrsync://rsync.macports.org/macports/release/tarballs/ports.tar [default]\n", string(conf))
	require.Empty(t, build.dependencies, "the person's own ports' archives aren't kept")
	changes, err := l.Changes(t.Context())
	require.NoError(t, err)
	require.Empty(t, changes)
}

// A check stopped mid-build has its prefix put back as it ends; one whose
// process ended first left its directory, which says the prefix is
// changed, with how to put it back, and the next check there puts it back
// before it builds.
func TestALocalCheckLeftUnfinishedIsPutBack(t *testing.T) {
	t.Parallel()
	path, active := registry(t, "  zlib @1.3.2_0 (active)\n")
	prefix, env, _ := fakePrefix(t, active, "HANG=fetch:harbor-cli")
	l := localProvider(t, env)
	ctx, cancel := context.WithCancel(t.Context())
	build := &fakeBuild{}
	done := make(chan error, 1)
	go func() { done <- l.Execute(ctx, localJob(t, prefix), build) }()
	require.Eventually(t, func() bool { return strings.Contains(readRegistry(t, path), "libharbor") }, testsupport.Settle, 10*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.Equal(t, "  zlib @1.3.2_0 (active)\n", readRegistry(t, path), "a stopped check puts the prefix back")
	require.Contains(t, build.progress, "put "+prefix+" back as it was")

	// A directory a process left that ended first.
	left := leftBehind(t, l.Temp, prefix)
	require.NoError(t, os.WriteFile(path, []byte("  zlib @1.3.2_0 (active)\n  libharbor @9.9_0 (active)\n"), 0o644))
	changes, err := l.Changes(t.Context())
	require.NoError(t, err)
	require.Equal(t, []buildenv.Change{{Check: "check-9", Place: prefix, Remedy: "dockhand undo check-9"}}, changes)

	l = localProvider(t, append(slices.DeleteFunc(slices.Clone(env), func(v string) bool { return strings.HasPrefix(v, "HANG=") }), "HANG="))
	l.Temp = filepath.Dir(left)
	build = &fakeBuild{}
	require.NoError(t, l.Execute(t.Context(), localJob(t, prefix), build))
	require.Contains(t, build.progress, "putting "+prefix+" back as it was before an earlier check, which ended before it could")
	require.NoDirExists(t, left)
	require.Equal(t, "  zlib @1.3.2_0 (active)\n", readRegistry(t, path))
}

// A guest program killed by a signal dockhand didn't send, as kill -9 of
// its group, isn't tried again: a kill is someone's choice, said once,
// and the prefix is put back; one that exited with a status unfinished,
// as a crash, is tried again, and dockhand's own stop is its own (rc7's
// G9 on the M1, 2026-11-02).
func TestAGuestKilledFromOutsideIsntTriedAgain(t *testing.T) {
	t.Parallel()
	path, active := registry(t, "  zlib @1.3.2_0 (active)\n")
	prefix, env, _ := fakePrefix(t, active, "HANG=fetch:harbor-cli")
	l := localProvider(t, env)
	build := &fakeBuild{}
	done := make(chan error, 1)
	go func() { done <- l.Execute(t.Context(), localJob(t, prefix), build) }()
	require.Eventually(t, func() bool { return strings.Contains(readRegistry(t, path), "libharbor") }, testsupport.Settle, 10*time.Millisecond)
	build.mu.Lock()
	dir := build.refs[0]
	build.mu.Unlock()
	found, err := exec.Command("pgrep", "-f", filepath.Join(dir, "guest.tcl")).Output()
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.Fields(string(found))[0])
	require.NoError(t, err)
	require.NoError(t, syscall.Kill(-pid, syscall.SIGKILL), "the guest's whole group, as G9 killed it")
	err = <-done
	require.ErrorIs(t, err, buildenv.ErrNeedsAttention)
	require.True(t, buildenv.SaysWhyNotTried(err))
	require.EqualError(t, err, "the guest program in "+prefix+" was killed by a signal dockhand didn't send (signal: killed); a kill is someone's choice, so no other attempt was made, and dockhand check runs it again")
	require.Equal(t, "  zlib @1.3.2_0 (active)\n", readRegistry(t, path), "the put-back as it is")

	// A crash, an exit with a status, is tried again; dockhand's own stop
	// is its own.
	crashed := &localRun{err: exec.Command("/bin/sh", "-c", "exit 1").Run()}
	require.NoError(t, crashed.killed(prefix))
	signaled := exec.Command("/bin/sh", "-c", "kill -9 $$").Run()
	require.Error(t, (&localRun{err: signaled}).killed(prefix))
	stopped := &localRun{err: signaled}
	stopped.stopped.Store(true)
	require.NoError(t, stopped.killed(prefix))
}

// dockhand undo check-N puts back a prefix a check left changed, or kept
// its versions active in, by the check's own put-back from its record,
// where port restore brings back requested ports alone; a kept check's
// directory stays, said as kept, and the next check there leaves it to
// undo (G2's rerun on rc8, 2026-10-29).
func TestUndoPutsBackAPrefixACheckLeftOrKept(t *testing.T) {
	t.Parallel()
	path, active := registry(t, "  zlib @1.3.2_0 (active)\n")
	prefix, env, _ := fakePrefix(t, active, "HANG=")
	l := localProvider(t, env)
	said := func(string) {}

	_, found, err := l.PutBack(t.Context(), "check-9", said)
	require.NoError(t, err)
	require.False(t, found, "nothing left")

	left := leftBehind(t, l.Temp, prefix)
	require.NoError(t, os.WriteFile(path, []byte("  zlib @1.3.2_0 (active)\n  libharbor @9.9_0 (active)\n"), 0o644))
	place, found, err := l.PutBack(t.Context(), "check-9", said)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, prefix, place)
	require.Equal(t, "  zlib @1.3.2_0 (active)\n", readRegistry(t, path), "put back as it was")
	require.NoDirExists(t, left)

	// A kept check's directory stays, said as kept; the next check there
	// leaves it; undo puts the prefix back.
	kept := leftBehind(t, l.Temp, prefix)
	require.NoError(t, os.WriteFile(filepath.Join(kept, "results.json"), []byte(`{"protocol":2,"state":"finished","environment":{"snapshot":"7","restored":"kept"}}`), 0o644))
	require.NoError(t, os.WriteFile(path, []byte("  zlib @1.3.2_0 (active)\n  libharbor @9.9_0 (active)\n"), 0o644))
	changes, err := l.Changes(t.Context())
	require.NoError(t, err)
	require.Equal(t, []buildenv.Change{{Check: "check-9", Place: prefix, Kept: true, Remedy: "dockhand undo check-9"}}, changes)
	require.NoError(t, l.restoreLeft(t.Context(), prefix, &fakeBuild{}))
	require.DirExists(t, kept, "the next check leaves a kept check's directory to undo")
	_, found, err = l.PutBack(t.Context(), "check-9", said)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "  zlib @1.3.2_0 (active)\n", readRegistry(t, path))
	require.NoDirExists(t, kept)
}

// Where dockhand's put-back can't run, port restore is offered as what it
// is, the ports requested before, with the rest that were active from the
// check's record.
func TestAPutBackByHandSaysWhatPortRestoreDoes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "before.txt"), []byte("  zlib @1.3.2_0 (active)\n  gettext @0.22.5_0 (active)\n  jq @1.7.1_0 (inactive)\n"), 0o644))
	require.Equal(t, "If dockhand cannot restore the prefix, sudo port restore --snapshot-id 7 restores the ports requested before check-9 and their dependencies. To restore the other ports that were active, run sudo port activate gettext @0.22.5_0 zlib @1.3.2_0",
		(&Local{}).byHand(dir, "check-9", "7"))
}

// leftBehind is a check's directory in temp that a process left, ending
// before it put the prefix back: its snapshot made, and zlib active
// before it.
func leftBehind(t *testing.T, temp, prefix string) string {
	t.Helper()
	left := filepath.Join(temp, localDirectory+"left")
	require.NoError(t, os.MkdirAll(left, 0o755))
	owned, err := json.Marshal(owner{PID: 0, Check: "check-9", Prefix: prefix})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(left, "owner.json"), owned, 0o644))
	earlier := asIs(guest.Local{})
	earlier.Protocol, earlier.Prefix = guest.Protocol, prefix
	input, err := json.Marshal(earlier)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(left, "input.json"), input, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(left, "guest.tcl"), guest.Program, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(left, "results.json"), []byte(`{"protocol":2,"state":"running","environment":{"snapshot":"7"}}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(left, "before.txt"), []byte("  zlib @1.3.2_0 (active)\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(left, "macports.conf"), nil, 0o644))
	return left
}

// A prefix the person put back themselves, with port restore, is no
// longer said to be changed, and what said so goes.
func TestAPrefixPutBackByHandIsForgotten(t *testing.T) {
	t.Parallel()
	_, active := registry(t, "  zlib @1.3.2_0 (active)\n")
	prefix, env, _ := fakePrefix(t, active)
	l := localProvider(t, env)
	left := leftBehind(t, l.Temp, prefix)
	changes, err := l.Changes(t.Context())
	require.NoError(t, err)
	require.Empty(t, changes)
	require.NoDirExists(t, left)
}

// Every root step runs one command, the prefix's port-tclsh, with the
// check's directory as its argument, which the probe asks sudo about, so
// one sudoers line lets serve check in a prefix whose registry is root's;
// where sudo would ask with no terminal to ask on, the refusal gives that
// line (the person's sitting on rc8, 2026-10-29).
func TestRootStepsRunThePrefixsPortTclsh(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root writes a registry whatever its mode, so no step asks sudo")
	}
	prefix := t.TempDir()
	registry := filepath.Join(prefix, "var", "macports", "registry")
	require.NoError(t, os.MkdirAll(registry, 0o755))
	require.NoError(t, os.Chmod(registry, 0o555))
	t.Cleanup(func() { os.Chmod(registry, 0o755) })
	called := filepath.Join(t.TempDir(), "called")
	answer := filepath.Join(t.TempDir(), "answer")
	sudo := filepath.Join(t.TempDir(), "sudo")
	testsupport.WriteExecutable(t, sudo, "#!/bin/sh\necho \"$*\" >> "+called+"\nexit $(cat "+answer+")\n")
	l := &Local{sudo: []string{sudo}, Env: []string{"USER=dhtest", "PATH=/usr/bin:/bin"}}
	tclsh := filepath.Join(prefix, "bin", "port-tclsh")

	require.NoError(t, os.WriteFile(answer, []byte("0"), 0o644))
	as, err := l.asRoot(t.Context(), prefix, &fakeBuild{})
	require.NoError(t, err)
	require.Equal(t, []string{sudo, "-n"}, as)
	said, err := os.ReadFile(called)
	require.NoError(t, err)
	require.Equal(t, "-n "+tclsh+" /dev/null\n", string(said), "the probe asks of the command every root step runs")

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "owner.json"), nil, 0o644))
	run, err := l.start(dir, prefix, "check-1", as)
	require.NoError(t, err)
	<-run.Done()
	said, err = os.ReadFile(called)
	require.NoError(t, err)
	require.Equal(t, "-n "+tclsh+" "+filepath.Join(dir, "guest.tcl")+" "+dir, strings.Split(strings.TrimSpace(string(said)), "\n")[1], "one command, no /usr/bin/env")

	l.terminal = filepath.Join(t.TempDir(), "no-terminal")
	require.NoError(t, os.WriteFile(answer, []byte("1"), 0o644))
	_, err = l.asRoot(t.Context(), prefix, &fakeBuild{})
	require.ErrorIs(t, err, buildenv.ErrNeedsAttention)
	require.ErrorContains(t, err, "This sudoers line, added with visudo, lets it: dhtest ALL=(root) NOPASSWD: "+tclsh+".")
	require.ErrorContains(t, err, "A prefix MacPorts installed without root needs no sudo")
}

// Where sudo refuses the person outright, whatever their password, the
// refusal says so, and that a rule for the prefix's port-tclsh is enough,
// one asking a password included, beside the one serve needs; a wrong
// password's refusal stays as it was, sudo's words on the terminal and
// in it (rc7's G9 on the M1, 2026-11-02).
func TestSudoRefusingThePersonSaysWhatRuleIsEnough(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root writes a registry whatever its mode, so no step asks sudo")
	}
	prefix := t.TempDir()
	registry := filepath.Join(prefix, "var", "macports", "registry")
	require.NoError(t, os.MkdirAll(registry, 0o755))
	require.NoError(t, os.Chmod(registry, 0o555))
	t.Cleanup(func() { os.Chmod(registry, 0o755) })
	says := filepath.Join(t.TempDir(), "says")
	sudo := filepath.Join(t.TempDir(), "sudo")
	testsupport.WriteExecutable(t, sudo, "#!/bin/sh\n[ \"$1\" = -v ] || exit 1\ncat "+says+" >&2\nexit 1\n")
	terminal := filepath.Join(t.TempDir(), "tty")
	require.NoError(t, os.WriteFile(terminal, nil, 0o644))
	l := &Local{sudo: []string{sudo}, terminal: terminal, Env: []string{"USER=dhtest", "PATH=/usr/bin:/bin"}}
	tclsh := filepath.Join(prefix, "bin", "port-tclsh")

	require.NoError(t, os.WriteFile(says, []byte("Sorry, user dhtest may not run sudo on m1.\n"), 0o644))
	_, err := l.asRoot(t.Context(), prefix, &fakeBuild{})
	require.ErrorIs(t, err, buildenv.ErrNeedsAttention)
	require.True(t, strings.HasPrefix(err.Error(), "dhtest may not use sudo on this Mac, and "+prefix+"'s registry is root's"), "its own words alone, said once: %v", err)
	require.ErrorContains(t, err, "one asking a password included, since a check run here has a terminal to ask on: dhtest ALL=(root) "+tclsh+".")
	require.ErrorContains(t, err, "serve, which has none, needs the rule to ask no password: dhtest ALL=(root) NOPASSWD: "+tclsh)
	seen, err := os.ReadFile(terminal)
	require.NoError(t, err)
	require.Equal(t, "Sorry, user dhtest may not run sudo on m1.\n", string(seen), "sudo's own words reach the terminal")

	require.NoError(t, os.WriteFile(says, []byte("Sorry, try again.\nsudo: 3 incorrect password attempts\n"), 0o644))
	_, err = l.asRoot(t.Context(), prefix, &fakeBuild{})
	require.ErrorIs(t, err, buildenv.ErrNeedsAttention)
	require.True(t, strings.HasPrefix(err.Error(), "sudo didn't let the build run as root in "+prefix+": sudo -v: exit status 1: Sorry, try again."), err.Error())
	require.NotContains(t, err.Error(), "may not use sudo")
}

// A check's directory dockhand can't remove, as one its guest program,
// root, made a directory in, is said, with how to remove it (G4 on rc8,
// 2026-10-29).
func TestADirectoryThatCantBeRemovedIsSaid(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root removes anything")
	}
	dir := filepath.Join(t.TempDir(), localDirectory+"stuck")
	locked := filepath.Join(dir, "rootmade")
	require.NoError(t, os.MkdirAll(locked, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(locked, "results.json"), nil, 0o644))
	require.NoError(t, os.Chmod(locked, 0o555))
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	build := &fakeBuild{}
	remove(dir, build)
	require.Len(t, build.progress, 1)
	require.Contains(t, build.progress[0], dir+" couldn't be removed: ")
	require.Contains(t, build.progress[0], "; sudo rm -rf "+dir+" removes it")
}

// A check whose last record can't be read keeps its directory, the
// prefix's only record of what was active before, and says how to look:
// a read error isn't evidence it changed nothing, and the directory was
// removed as though it were (Codex's sixth review, #69, its probes).
func TestAnUnreadableResultKeepsThePrefixsRecord(t *testing.T) {
	t.Parallel()
	directory := filepath.Join(t.TempDir(), "check-that-changed-prefix")
	require.NoError(t, os.Mkdir(directory, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "before.txt"), []byte("  original @1_0 (active)\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "snapshot.log"), []byte("snapshot 7 created\n"), 0644))
	follower := &guest.Follower{Root: directory}
	require.NoError(t, os.WriteFile(filepath.Join(directory, "results.json"), []byte(`{"protocol":2,"state":"running","environment":{"snapshot":"7"},"targets":[]}`), 0644))
	earlier, err := follower.Read(t.Context(), hostGuest{})
	require.NoError(t, err)
	require.Equal(t, "7", earlier.Environment["snapshot"])
	// Simulate an unreadable final result after the guest took a snapshot
	// and changed the active ports. A read error is not evidence that it
	// never reached the snapshot or mutation.
	require.NoError(t, os.WriteFile(filepath.Join(directory, "results.json"), []byte("{corrupt final result"), 0644))
	_, err = follower.Read(t.Context(), hostGuest{})
	require.Error(t, err)
	build := &fakeBuild{}
	(&Local{}).settle(t.Context(), follower, directory, "/fake-prefix", "check-7", nil, build)
	require.DirExists(t, directory)
	require.Contains(t, strings.Join(build.progress, "\n"), "check-7's record of how it left /fake-prefix can't be read")
}

// The next check on a prefix an unreadable record left is refused, saying
// how to look, and the record stays; one on another prefix goes on.
func TestAnUnreadableRecordRefusesTheNextCheckThere(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	directory := filepath.Join(root, localDirectory+"damaged")
	require.NoError(t, os.Mkdir(directory, 0755))
	before := filepath.Join(directory, "before.txt")
	require.NoError(t, os.WriteFile(before, []byte("  original @1_0 (active)\n"), 0644))
	data, err := json.Marshal(owner{PID: 999999, Start: "darwin:1.0", Check: "check-7", Prefix: "/fake-prefix"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "owner.json"), data, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "results.json"), []byte("{corrupt final result"), 0644))
	build := &fakeBuild{}
	err = (&Local{Temp: root}).restoreLeft(t.Context(), "/fake-prefix", build)
	_, retained := os.Stat(before)
	require.NoError(t, retained)
	require.ErrorContains(t, err, "check-7's record of how it left /fake-prefix can't be read")
	require.NoError(t, (&Local{Temp: root}).restoreLeft(t.Context(), "/other-prefix", build))
	require.FileExists(t, before)
}
