package tart

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// asIs is two targets checked in a prefix of the person's, as the local
// provider asks: among whatever is active, put back after.
func asIs(local guestLocal) guestInput {
	input := twoTargets("declared")
	local.Note = "dockhand check-1 attempt 1"
	input.Local = &local
	return input
}

// registry is a fake port's registry of active ports, as ACTIVEFILE has
// it, holding these lines.
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

// In a prefix of the person's, nothing is deactivated before a target
// builds, and nothing is refused for what's there: how many ports are
// active, and another package manager, are facts of the build. A snapshot
// is made before anything changes, and the prefix is put back after:
// each port whose active version changed activated again at its old one,
// each the check activated deactivated, and the snapshot, its work done,
// deleted. The prefix's etc/macports is never written.
func TestALocalCheckBuildsAsIsAndPutsThePrefixBack(t *testing.T) {
	t.Parallel()
	before := []string{"  zlib @1.3.2_0 (active)\n", "  libharbor @3_0 (active)\n"}
	path, active := registry(t, before...)
	homebrew := t.TempDir()
	root := t.TempDir()
	results, commands := guestRunIn(t, root, asIs(guestLocal{}), active, "DEPS=libharbor=zlib harbor-cli=libharbor", "FOREIGN="+homebrew)
	require.Equal(t, "finished", results.State, results.Detail)
	require.Equal(t, "passed", results.Targets[1].Outcome)
	require.Equal(t, "7", results.Environment["snapshot"])
	require.Equal(t, "2", results.Environment["active_ports"])
	require.Equal(t, homebrew, results.Environment["foreign"])
	require.Equal(t, "yes", results.Environment["restored"])
	require.NotContains(t, commands, "-N -f deactivate active", "nothing is deactivated before a target builds")
	require.Equal(t, "-N snapshot --create --note dockhand check-1 attempt 1", commands[slices.IndexFunc(commands, func(c string) bool { return strings.Contains(c, "snapshot --create") })])
	require.Less(t, slices.Index(commands, "-N snapshot --create --note dockhand check-1 attempt 1"), slices.IndexFunc(commands, func(c string) bool { return strings.Contains(c, " clean --work ") }),
		"the snapshot comes before anything changes")
	require.Contains(t, commands, "-N activate libharbor @3_0")
	require.Contains(t, commands, "-N -f deactivate harbor-cli")
	require.Contains(t, commands, "-N snapshot --delete 7")
	got := readRegistry(t, path)
	for _, line := range before {
		require.Contains(t, got, line)
	}
	require.NotContains(t, got, "harbor-cli", "what the check activated is deactivated")
	require.NoFileExists(t, filepath.Join(root, "prefix", "etc", "macports", "sources.conf"))
}

// Keeping what the check built leaves the branch's versions active, as
// check --keep and [providers.local] keep ask, and its snapshot standing
// for a put-back of the person's own.
func TestALocalCheckKeepsWhatItBuiltWhereAsked(t *testing.T) {
	t.Parallel()
	path, active := registry(t, "  libharbor @3_0 (active)\n")
	results, commands := guestRun(t, asIs(guestLocal{Keep: true}), active, "NEWVERSION=4_0")
	require.Equal(t, "finished", results.State, results.Detail)
	require.Equal(t, "kept", results.Environment["restored"])
	require.NotContains(t, commands, "-N activate libharbor @3_0")
	require.NotContains(t, commands, "-N snapshot --delete 7")
	require.Contains(t, readRegistry(t, path), "  libharbor @4_0 (active)\n")
	require.Contains(t, readRegistry(t, path), "  harbor-cli @4_0 (active)\n")
}

// A prefix dockhand may clean builds each target as a clone does, all but
// its dependencies deactivated, and is put back after.
func TestALocalCheckInAPrefixItMayCleanBuildsClean(t *testing.T) {
	t.Parallel()
	path, active := registry(t, "  zlib @1.3.2_0 (active)\n", "  xz @5.8.1_0 (active)\n")
	results, commands := guestRun(t, asIs(guestLocal{Clean: true}), active, "DEPS=libharbor=zlib harbor-cli=libharbor")
	require.Equal(t, "finished", results.State, results.Detail)
	require.Contains(t, commands, "-N -f deactivate active")
	require.Equal(t, "yes", results.Environment["restored"])
	got := readRegistry(t, path)
	require.Contains(t, got, "  zlib @1.3.2_0 (active)\n")
	require.Contains(t, got, "  xz @5.8.1_0 (active)\n")
	require.NotContains(t, got, "libharbor")
}

// An image of the version the branch builds, installed already, would be
// activated rather than built: the check stops, saying so, and puts the
// prefix back.
func TestALocalCheckRefusesWhereTheBranchsVersionIsInstalled(t *testing.T) {
	t.Parallel()
	_, active := registry(t, "  zlib @1.3.2_0 (active)\n")
	results, commands := guestRun(t, asIs(guestLocal{}), active, "DEPVERSION=libharbor=4,0", "INSTALLED=  libharbor @4_0\\n")
	require.Equal(t, "refused", results.State)
	require.Contains(t, results.Detail, "libharbor @4_0 is installed in")
	require.Contains(t, results.Detail, "port uninstall libharbor @4_0")
	require.Equal(t, "yes", results.Environment["restored"])
	require.False(t, slices.ContainsFunc(commands, func(c string) bool { return strings.Contains(c, "-dkns install") }), "nothing is built")
}

// A check whose process ended before it put the prefix back is put back
// by the program run to do only that, from what was active before.
func TestTheGuestPutsBackWhatAnEarlierCheckLeft(t *testing.T) {
	t.Parallel()
	path, active := registry(t, "  libharbor @4_0 (active)\n", "  harbor-cli @4_0 (active)\n")
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "before.txt"), []byte("  libharbor @3_0 (active)\n  zlib @1.3.2_0 (active)\n"), 0o644))
	results, commands := guestRunIn(t, root, asIs(guestLocal{Restore: true}), active)
	require.Equal(t, "finished", results.State, results.Detail)
	require.Equal(t, "yes", results.Environment["restored"])
	require.Contains(t, commands, "-N -f deactivate harbor-cli")
	require.Contains(t, commands, "-N activate libharbor @3_0")
	require.Contains(t, commands, "-N activate zlib @1.3.2_0")
	got := readRegistry(t, path)
	require.Contains(t, got, "  libharbor @3_0 (active)\n")
	require.Contains(t, got, "  zlib @1.3.2_0 (active)\n")
	require.NotContains(t, got, "harbor-cli")
}

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
	require.NoError(t, os.WriteFile(filepath.Join(prefix, "bin", "port"), []byte(fakePort), 0o755))
	prelude := filepath.Join(root, "prelude.tcl")
	require.NoError(t, os.WriteFile(prelude, []byte(`
package provide macports 1.0
namespace eval macports {variable os_platform darwin; variable os_major 25; variable build_arch arm64}
proc mportinit {} {}
proc declares_tests {portdir name variants} { return 0 }
proc checkout {portdir name variants} { return "" }
proc work_dir {portdir name variants} { return "" }
set foreignManagers {}
set argv0 [lindex $argv 0]
set argv [lrange $argv 1 end]
source $argv0
`), 0o644))
	tclsh := testsupport.MacPortsTclsh(t)
	require.NoError(t, os.WriteFile(filepath.Join(prefix, "bin", "port-tclsh"), []byte("#!/bin/sh\nexec "+tclsh+" "+prelude+" \"$@\"\n"), 0o755))
	otool := filepath.Join(root, "otool")
	require.NoError(t, os.WriteFile(otool, []byte(fakeOtool), 0o755))
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
		stager: func(_ context.Context, _ buildenv.Job, input guestInput, dir string) error {
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
			if err := os.WriteFile(filepath.Join(dir, "guest.tcl"), guestProgram, 0o644); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, "input.json"), data, 0o644)
		}}
}

func localJob(t *testing.T, prefix string) buildenv.Job {
	job := tartJob(t, 1)
	job.Environment = model.Environment{Provider: buildenv.Local, Platform: tahoe, Prefix: prefix}
	return job
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
	require.Eventually(t, func() bool { return strings.Contains(readRegistry(t, path), "libharbor") }, 10*time.Second, 10*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.Equal(t, "  zlib @1.3.2_0 (active)\n", readRegistry(t, path), "a stopped check puts the prefix back")
	require.Contains(t, build.progress, "put "+prefix+" back as it was")

	// A directory a process left that ended first.
	left := leftBehind(t, l.Temp, prefix)
	require.NoError(t, os.WriteFile(path, []byte("  zlib @1.3.2_0 (active)\n  libharbor @9.9_0 (active)\n"), 0o644))
	changes, err := l.Changes(t.Context())
	require.NoError(t, err)
	require.Equal(t, []buildenv.Change{{Check: "check-9", Place: prefix, Remedy: "sudo port restore --snapshot-id 7"}}, changes)

	l = localProvider(t, append(slices.DeleteFunc(slices.Clone(env), func(v string) bool { return strings.HasPrefix(v, "HANG=") }), "HANG="))
	l.Temp = filepath.Dir(left)
	build = &fakeBuild{}
	require.NoError(t, l.Execute(t.Context(), localJob(t, prefix), build))
	require.Contains(t, build.progress, "putting "+prefix+" back as it was before an earlier check, which ended before it could")
	require.NoDirExists(t, left)
	require.Equal(t, "  zlib @1.3.2_0 (active)\n", readRegistry(t, path))
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
	earlier := asIs(guestLocal{})
	earlier.Protocol, earlier.Prefix = Protocol, prefix
	input, err := json.Marshal(earlier)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(left, "input.json"), input, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(left, "guest.tcl"), guestProgram, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(left, "results.json"), []byte(`{"protocol":1,"state":"running","environment":{"snapshot":"7"}}`), 0o644))
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
