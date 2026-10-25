package tart

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// fakeOtool stands for Xcode's otool: it prints a file's -L, -l, or -D
// answer from the file beside it, harbor.links, harbor.loads, or
// harbor.id (named apart, since a Mac's file system folds case).
const fakeOtool = `#!/bin/sh
case "$1" in
  -L) cat "$2.links" ;;
  -l) cat "$2.loads" 2>/dev/null; exit 0 ;;
  -D) cat "$2.id" 2>/dev/null; exit 0 ;;
esac
`

// fakePort stands for MacPorts' port in a guest: it records each command,
// has the ports ACTIVE lists active ("  name @spec (active)" lines), prints
// each target's dependencies from DEPS ("name=dep ..."), and fails the
// phases FAIL names ("phase:port ..."), with MacPorts' closing Error
// lines after the failure's own, or with no Error line at all where QUIET
// is set. Each port's dependencies other than for building, as rdeps
// --no-build has them, come from RDEPS ("name=dep,dep ..."). A port's
// archive is ARCHIVES/<name>, and
// its directory devel/<name>, but for UNRESOLVED, which port can't
// resolve. Each step of a build writes its command to the log, "port
// <arguments>", installing dependencies writes DEPLINES lines more, and
// lint writes LINTSAYS, an Error line lint says and goes on past. A
// phase HANG names ("phase:port ...") sleeps, as a compiler that never
// finishes would, in a process of its own. A port NOARCHIVE names has no
// binary archive, which -b archivefetch fails for. With COMMAND set, a
// failure says FAILSAYS as the tool's line, where it's set. With
// ACTIVEFAILS set,
// listing the active ports fails. With ACTIVEFILE set, it's a registry
// of its own, as a prefix of the person's is: the active ports are that
// file's lines, which deactivating, activating, and installing change, a
// target's install activating it at NEWVERSION. INSTALLED lists the
// installed images of a port, as "-q installed <name>" does. Its port
// snapshot keeps the note it's made with, which --list says as ID 7.
// Where PORTSRC names a macports.conf, it's copied beside PORT_LOG once,
// with the sources.conf it names.
const fakePort = `#!/bin/sh
echo "$*" >> "$PORT_LOG"
if [ -n "$PORTSRC" ] && [ ! -f "$PORT_LOG.conf" ]; then cat "$PORTSRC" "$(awk '/^sources_conf/ {print $2}' "$PORTSRC")" > "$PORT_LOG.conf"; fi
drop() { for name in "$@"; do grep -v "^  $name @" "$ACTIVEFILE" > "$ACTIVEFILE.new"; mv "$ACTIVEFILE.new" "$ACTIVEFILE"; done; }
case "$*" in
  "-N snapshot --create --note "*) all="$*"; echo "${all#-N snapshot --create --note }" > "$DOCKHAND_GUEST_ROOT/snapshot.note"; exit 0 ;;
  "-q snapshot --list") if [ -f "$DOCKHAND_GUEST_ROOT/snapshot.note" ]; then printf '   7  2026-10-25 12:00:00+0000  %s\n' "$(cat "$DOCKHAND_GUEST_ROOT/snapshot.note")"; fi; exit 0 ;;
  "-N snapshot --delete "*) exit 0 ;;
esac
if [ -n "$ACTIVEFILE" ]; then
  case "$*" in
    *"-q installed active"*) cat "$ACTIVEFILE"; exit 0 ;;
    "-N -f deactivate active") : > "$ACTIVEFILE" ;;
    "-N -f deactivate "*) shift 3; drop "$@" ;;
    "-N activate "*) drop "$3"; echo "  $3 $4 (active)" >> "$ACTIVEFILE"; echo "port $*"; exit 0 ;;
    *"install --unrequested "*"subport="*)
      all="$*"; name=${all##*subport=}; name=${name%% *}; drop "$name"; echo "  $name @${NEWVERSION:-9.9_0} (active)" >> "$ACTIVEFILE" ;;
    "-N install --unrequested "*|"-N -d install --unrequested "*)
      for dep in $*; do case "$dep" in -*|install) ;; *) grep -q "^  $dep @" "$ACTIVEFILE" || echo "  $dep @1_0 (active)" >> "$ACTIVEFILE" ;; esac; done ;;
  esac
fi
case "$*" in
  *"-q installed active"*) ;;
  "-q installed "?*) printf '%b' "$INSTALLED"; exit 0 ;;
esac
case "$*" in
  *"-q installed active"*) if [ -n "$ACTIVEFAILS" ]; then echo "Error: registry locked" >&2; exit 1; fi; printf '%b' "$ACTIVE"; exit 0 ;;
  *"-q installed"*) exit 0 ;;
  *"-q contents "*) if [ -z "$CONTENTS" ]; then echo "Error: registry locked" >&2; exit 1; fi; printf '%b' "$CONTENTS"; exit 0 ;;
  "provides "*)
    shift
    for file in "$@"; do
      owner=""
      for entry in $PROVIDES; do [ "${entry%%=*}" = "$file" ] && owner=${entry#*=}; done
      if [ -n "$owner" ]; then echo "$file is provided by: $owner"; else echo "$file is not provided by a MacPorts port."; fi
    done
    exit 0 ;;
  "-q location "*)
    shift 2
    while [ $# -gt 0 ]; do echo "$ARCHIVES/$1"; shift 2; done
    exit 0 ;;
  "-q dir "*)
    shift 2
    for name in "$@"; do
      if [ "$name" = "$UNRESOLVED" ]; then echo "Error: Port $name not found" >&2; exit 1; fi
      echo "$DOCKHAND_GUEST_ROOT/ports/devel/$name"
    done
    exit 0 ;;
  *"echo depof:"*)
    for entry in $DEPS; do
      case "$*" in *"depof:${entry%%=*}") echo "${entry#*=}" | tr ',' ' ' ;; esac
    done
    exit 0 ;;
  "-N -b archivefetch "*)
    for name in $NOARCHIVE; do
      case "$*" in *" archivefetch $name") echo "Error: no archive for $name" >&2; exit 1 ;; esac
    done
    exit 0 ;;
  *"-q info --line --version --revision "*)
    for entry in $DEPVERSION; do
      case "$*" in *" ${entry%%=*}"|*"subport=${entry%%=*}") echo "${entry#*=}" | tr ',' '\t' ;; esac
    done
    exit 0 ;;
  *"-q rdeps --no-build "*)
    for entry in $RDEPS; do
      case "$*" in *"--no-build ${entry%%=*}") echo "${entry#*=}" | tr ',' '\n' | sed 's/^/  /' ;; esac
    done
    exit 0 ;;
esac
case "$*" in
  *" deactivate "*|*" clean "*|*" lint "*|*" install "*|*" fetch "*|*" checksum "*|*" test "*)
    echo "port $*"
    case "$*" in
      "-N -d install --unrequested "*|"-N install --unrequested "*)
        if [ "${DEPLINES:-0}" -gt 0 ]; then yes "a dependency's line" | head -n "$DEPLINES"; fi
        if [ -n "$DEPFAILS" ]; then
          echo "Error: Failed to build $DEPFAILS: command execution failed"
          echo "Error: See /opt/local/var/macports/logs/$DEPFAILS/main.log for details."
          exit 1
        fi ;;
      *" lint "*) if [ -n "$LINTSAYS" ]; then echo "$LINTSAYS"; fi ;;
    esac ;;
esac
for entry in $HANG; do
  phase=${entry%%:*}; port=${entry#*:}
  case "$*" in
    *" $phase "*"subport=$port"*) sleep 60 ;;
  esac
done
for entry in $FAIL; do
  phase=${entry%%:*}; port=${entry#*:}
  case "$*" in
    *" $phase "*"subport=$port"*|*" $phase "*"subport=$port "*)
      if [ -n "$QUIET" ]; then exit 1; fi
      if [ -n "$COMMAND" ]; then
        echo "DEBUG: Executing org.macports.build ($port)"
        echo "${FAILSAYS:-go: github.com/spf13/pflag: cannot find package}"
        echo "Command failed:  cd /work/$port && go build ./..."
        echo "Exit code: 1"
        echo "Error: Failed to build $port: command execution failed"
        echo "Error: See /opt/local/var/macports/logs/$port/main.log for details."
        exit 1
      fi
      echo "Error: Failed to $phase $port: it broke"
      echo "Error: See /opt/local/var/macports/logs/$port/main.log for details."
      echo "Error: Processing of port $port failed"
      exit 1 ;;
  esac
done
exit 0
`

// guestRun runs the shipped guest program on input, in a scratch root with
// the fake port, and returns its results and the commands port was given.
func guestRun(t *testing.T, input guestInput, env ...string) (guestResults, []string) {
	t.Helper()
	return guestRunIn(t, t.TempDir(), input, env...)
}

// guestRunIn is guestRun in a root the caller gives, whose prefix is
// root/prefix.
func guestRunIn(t *testing.T, root string, input guestInput, env ...string) (guestResults, []string) {
	t.Helper()
	command := guestCommand(t, root, input, env...)
	output, _ := command.CombinedOutput()
	results, err := readResults(root)
	require.NoError(t, err, "%s", output)
	commands, _ := os.ReadFile(filepath.Join(root, "port.log"))
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(commands)), "\n") {
		if line != "" {
			// The port directory is the scratch root's; the tail is enough.
			lines = append(lines, strings.ReplaceAll(line, filepath.Join(root, "ports")+"/", ""))
		}
	}
	return results, lines
}

// readResults reads the results the guest program has written in root.
func readResults(root string) (guestResults, error) {
	var results guestResults
	data, err := os.ReadFile(filepath.Join(root, "results.json"))
	if err != nil {
		return results, err
	}
	return results, json.Unmarshal(data, &results)
}

// guestCommand is the shipped guest program, ready to run on input in
// root, with the fake port.
func guestCommand(t *testing.T, root string, input guestInput, env ...string) *exec.Cmd {
	t.Helper()
	executable := testsupport.MacPortsTclsh(t)
	prefix := filepath.Join(root, "prefix")
	require.NoError(t, os.MkdirAll(filepath.Join(prefix, "bin"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(prefix, "etc", "macports"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(prefix, "bin", "port"), []byte(fakePort), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "ports"), 0o755))
	if !strings.Contains(strings.Join(env, " "), "NO_INDEX=1") {
		require.NoError(t, os.WriteFile(filepath.Join(root, "ports", "PortIndex"), nil, 0o644))
	}
	input.Protocol, input.Prefix = Protocol, prefix
	input.Platform = model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}
	if input.TestTimeout == 0 {
		input.TestTimeout = 60
	}
	if input.BuildTimeout == 0 {
		input.BuildTimeout = 3600
	}
	if input.LintTimeout == 0 {
		input.LintTimeout = 600
	}
	data, err := json.Marshal(input)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "input.json"), data, 0o644))
	// MacPorts itself is stood in for: the platform, which ports declare
	// tests, and where a Git fetch left each one's checkout.
	prelude := `
package provide macports 1.0
namespace eval macports {variable os_platform darwin; variable os_major 25; variable build_arch arm64}
proc mportinit {} {}
proc declares_tests {portdir name variants} { return [expr {$name in $::env(TESTED)}] }
proc checkout {portdir name variants} { return [file join $::env(CHECKOUTS) $name] }
proc work_dir {portdir name variants} { return [file join $::env(CHECKOUTS) $name-work] }
set foreignManagers [expr {[info exists ::env(FOREIGN)] ? $::env(FOREIGN) : {}}]
`
	script := filepath.Join(root, "guest.tcl")
	require.NoError(t, os.WriteFile(script, append([]byte(prelude), guestProgram...), 0o644))
	command := exec.CommandContext(t.Context(), executable, script)
	portLog := filepath.Join(root, "port.log")
	otool := filepath.Join(root, "otool")
	require.NoError(t, os.WriteFile(otool, []byte(fakeOtool), 0o755))
	command.Env = append(append(os.Environ(), "DOCKHAND_GUEST_ROOT="+root, "DOCKHAND_GUEST_OTOOL="+otool, "CONTENTS=", "PROVIDES=", "PORT_LOG="+portLog, "TESTED=", "DEPS=", "RDEPS=", "FAIL=", "ACTIVE=", "ARCHIVES="+root, "UNRESOLVED=", "DEPLINES=", "LINTSAYS=", "QUIET=", "HANG=", "NOARCHIVE=", "DEPFAILS=", "ACTIVEFILE=", "INSTALLED=", "DEPVERSION=", "CHECKOUTS="+filepath.Join(root, "checkouts")), env...)
	return command
}

func twoTargets(tests string) guestInput {
	return guestInput{Run: "check-1", Attempt: 1, Tests: tests, Targets: []guestTarget{
		{ID: "libharbor", Name: "libharbor", Portfile: "devel/libharbor/Portfile", Log: buildenv.LogName("libharbor")},
		{ID: "harbor-cli", Name: "harbor-cli", Portfile: "devel/harbor-cli/Portfile", DependsOn: []string{"libharbor"}, Log: buildenv.LogName("harbor-cli")},
	}}
}

// Each target's earlier work is cleaned, and it's linted, its
// dependencies installed, and then fetched, checksummed, and installed
// from its source, never from a published archive, as MacPorts CI does
// (install-port --source), with its declared tests after; the results say
// so target by target. s2n-tls's +tests build was refused for +debug's
// work, and a target MacPorts' packages had was installed from them, not
// built (the s2n-tls run's findings 1 and 2).
func TestTheGuestBuildsEachTargetInCIsOrder(t *testing.T) {
	t.Parallel()
	results, commands := guestRun(t, twoTargets("declared"), "TESTED=libharbor", "DEPS=libharbor=zlib harbor-cli=libharbor")
	require.Equal(t, "finished", results.State, results.Detail)
	steps := []guestStep{{"clean", 1}, {"lint", 2}, {"dependencies", 3}, {"fetch", 4}, {"checksum", 5}, {"install", 6}}
	require.Equal(t, []guestResult{
		{ID: "libharbor", Outcome: "passed", Tests: "passed", Log: "libharbor.log", Active: []guestPort{}, Steps: append(slices.Clone(steps), guestStep{"test", 7})},
		{ID: "harbor-cli", Outcome: "passed", Tests: "none", Log: "harbor-cli.log", Active: []guestPort{}, Steps: steps},
	}, results.Targets)
	// The guest says what uname -m says, here the Mac running the test's.
	require.Equal(t, map[string]string{"arm64": "arm64", "amd64": "x86_64"}[runtime.GOARCH], results.Environment["architecture"])
	var libharbor []string
	for _, command := range commands {
		if strings.Contains(command, "devel/libharbor") || strings.Contains(command, "depof:libharbor") {
			libharbor = append(libharbor, command)
		}
	}
	require.Equal(t, []string{
		"-N -D devel/libharbor clean --work subport=libharbor",
		"-N -D devel/libharbor lint subport=libharbor",
		"-q echo depof:libharbor",
		"-N -D devel/libharbor -d fetch subport=libharbor",
		"-N -D devel/libharbor -d checksum subport=libharbor",
		"-N -D devel/libharbor -dkns install --unrequested subport=libharbor",
		"-N -D devel/libharbor -d test subport=libharbor",
	}, keep(libharbor, func(c string) bool { return !strings.Contains(c, "installed") }))
	require.Contains(t, commands, "-N install --unrequested zlib", "a target's dependencies are installed first, quietly, from archives")
	require.Contains(t, commands, "-N install --unrequested libharbor", "the dependent's changed dependency is among them")
}

// Each target's result says where each step of its build begins in its
// log, the line its output starts on, as the guest runs the step, so a
// port's own build is found after its dependencies' installs: hugo's own
// phases began near line 46,400 of 47,000 (the hugo exercise). Each
// target's log is counted on its own, and the line named is the step's
// first.
func TestTheGuestRecordsWhereEachStepBeginsInItsLog(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	results, _ := guestRunIn(t, root, twoTargets("declared"), "TESTED=libharbor", "DEPS=libharbor=zlib harbor-cli=libharbor", "RDEPS=libharbor=zlib",
		"ACTIVE=  zlib @1.3.2_0 (active)\\n", "DEPLINES=46000")
	require.Equal(t, "finished", results.State, results.Detail)
	steps := []guestStep{{"deactivate", 1}, {"clean", 2}, {"lint", 3}, {"dependencies", 4}, {"fetch", 46005}, {"checksum", 46006}, {"install", 46007}}
	require.Equal(t, append(slices.Clone(steps), guestStep{"test", 46008}), results.Targets[0].Steps)
	require.Equal(t, steps, results.Targets[1].Steps, "its own log, counted from its start")
	commands := map[string]string{
		"deactivate": "-N -f deactivate active", "clean": " clean --work ", "lint": " lint ", "dependencies": "-N install --unrequested ",
		"fetch": " -d fetch ", "checksum": " -d checksum ", "install": " -dkns install ", "test": " -d test ",
	}
	for _, target := range results.Targets {
		data, err := os.ReadFile(filepath.Join(root, target.Log))
		require.NoError(t, err)
		lines := strings.Split(string(data), "\n")
		for _, step := range target.Steps {
			line := lines[step.Line-1]
			require.True(t, strings.HasPrefix(line, "port ") && strings.Contains(line, commands[step.Name]), "%s's %s step begins at line %d: %q", target.ID, step.Name, step.Line, line)
		}
	}

	// A target that fails has the steps it began, the one it failed at
	// last; one blocked has none.
	results, _ = guestRunIn(t, t.TempDir(), twoTargets("declared"), "FAIL=checksum:libharbor")
	require.Equal(t, []guestStep{{"clean", 1}, {"lint", 2}, {"fetch", 3}, {"checksum", 4}}, results.Targets[0].Steps)
	require.Equal(t, "blocked", results.Targets[1].Outcome)
	require.Empty(t, results.Targets[1].Steps)
}

// Each verdict comes with what its build read (decision 28): the ports
// active as it built, other than itself, each with where its name resolves
// and its archive's digest, and the target's own archive's digest. A port
// kept as a directory has no digest, and a name port can't resolve has no
// directory.
func TestAVerdictRecordsThePortsActiveAsItBuilt(t *testing.T) {
	t.Parallel()
	archives := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(archives, "zlib"), []byte("zlib's archive"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(archives, "libharbor"), []byte("libharbor's archive"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(archives, "xz"), 0o755))
	active := `  zlib @1.3.2_0 (active)\n  xz @5.8.1_0+universal (active)\n  gone @1.0_0 (active)\n  libharbor @3_0 (active)\n`
	input := guestInput{Run: "check-1", Attempt: 1, Tests: "skip", Targets: []guestTarget{{ID: "libharbor", Name: "libharbor", Portfile: "devel/libharbor/Portfile"}}}
	results, commands := guestRun(t, input, "ACTIVE="+active, "ARCHIVES="+archives, "UNRESOLVED=gone")
	require.Equal(t, "finished", results.State, results.Detail)
	digest := func(data string) string {
		sum := sha256.Sum256([]byte(data))
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	require.Equal(t, []guestPort{
		{Name: "zlib", Spec: "@1.3.2_0", Directory: "devel/zlib", Archive: digest("zlib's archive"), ArchiveFile: filepath.Join(archives, "zlib")},
		{Name: "xz", Spec: "@5.8.1_0+universal", Directory: "devel/xz"},
		{Name: "gone", Spec: "@1.0_0"},
	}, results.Targets[0].Active)
	require.Equal(t, digest("libharbor's archive"), results.Targets[0].Archive)
	require.Equal(t, filepath.Join(archives, "libharbor"), results.Targets[0].ArchiveFile, "where it is, for the host to keep it")
	require.Contains(t, commands, "-q location zlib @1.3.2_0 xz @5.8.1_0+universal gone @1.0_0", "asked once for all of them")
	require.Contains(t, commands, "-q dir gone", "asked alone once the list failed")

	// A list of the active ports that fails leaves what the build read
	// unknown, never none, which reuse took as complete (Codex's review,
	// checked against a268b6e5).
	results, _ = guestRun(t, input, "ACTIVE="+active, "ARCHIVES="+archives, "ACTIVEFAILS=1")
	require.Equal(t, "finished", results.State, results.Detail)
	require.Equal(t, "passed", results.Targets[0].Outcome, "the verdict stands")
	require.Nil(t, results.Targets[0].Active, "absent, its inputs unknown")
}

// A target that fails stops at its phase, and a target that needs it is
// blocked rather than built against an old build of it.
func TestAFailedTargetBlocksWhatNeedsIt(t *testing.T) {
	t.Parallel()
	results, commands := guestRun(t, twoTargets("declared"), "FAIL=fetch:libharbor")
	require.Equal(t, "finished", results.State)
	require.Equal(t, "failed", results.Targets[0].Outcome)
	require.Equal(t, "fetch", results.Targets[0].Phase)
	require.Equal(t, "Failed to fetch libharbor: it broke", results.Targets[0].Detail)
	require.Equal(t, "blocked", results.Targets[1].Outcome)
	for _, command := range commands {
		require.NotContains(t, command, "subport=harbor-cli", "the blocked target isn't built")
	}
}

// A failed test phase brings back the last lines of the test logs the
// port's build systems left in its work directory, which goes with the VM,
// each under a dockhand: line: exiv2's said only where CTest's log was
// (the exiv2 field notes, 2026-10-19). A passing one brings none.
func TestAFailedTestPhaseBringsBackTheTestsOwnLogs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	work := filepath.Join(root, "checkouts", "libharbor-work")
	ctest := filepath.Join(work, "build", "Testing", "Temporary", "LastTest.log")
	require.NoError(t, os.MkdirAll(filepath.Dir(ctest), 0o755))
	require.NoError(t, os.WriteFile(ctest, []byte("3/8 Testing: bugfixes-test\nbugfixes-test: expected 4, got 5\n"), 0o644))
	results, _ := guestRunIn(t, root, twoTargets("declared"), "TESTED=libharbor", "FAIL=test:libharbor")
	require.Equal(t, "failed", results.Targets[0].Tests)
	log, err := os.ReadFile(filepath.Join(root, "libharbor.log"))
	require.NoError(t, err)
	require.Contains(t, string(log), "dockhand: the last lines of build/Testing/Temporary/LastTest.log, the tests' own log:\n3/8 Testing: bugfixes-test\nbugfixes-test: expected 4, got 5\n")

	passing := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(passing, "checkouts", "libharbor-work"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(passing, "checkouts", "libharbor-work", "test-suite.log"), []byte("PASS: all\n"), 0o644))
	results, _ = guestRunIn(t, passing, twoTargets("declared"), "TESTED=libharbor")
	require.Equal(t, "passed", results.Targets[0].Tests)
	log, err = os.ReadFile(filepath.Join(passing, "libharbor.log"))
	require.NoError(t, err)
	require.NotContains(t, string(log), "the tests' own log")
}

// Declared tests are advisory: a failure is reported, and the target still
// passes. Required tests decide.
func TestTestsAreAdvisoryUnlessRequired(t *testing.T) {
	t.Parallel()
	results, _ := guestRun(t, twoTargets("declared"), "TESTED=libharbor", "FAIL=test:libharbor")
	require.Equal(t, "passed", results.Targets[0].Outcome)
	require.Equal(t, "failed", results.Targets[0].Tests)
	require.Equal(t, "passed", results.Targets[1].Outcome, "an advisory test failure blocks nothing")
	results, _ = guestRun(t, twoTargets("required"), "TESTED=libharbor", "FAIL=test:libharbor")
	require.Equal(t, "failed", results.Targets[0].Outcome)
	require.Equal(t, "test", results.Targets[0].Phase)
	require.Equal(t, "blocked", results.Targets[1].Outcome)
	results, commands := guestRun(t, twoTargets("skip"), "TESTED=libharbor")
	require.Equal(t, "skipped", results.Targets[0].Tests)
	for _, command := range commands {
		require.NotContains(t, command, " test ")
	}
}

// A failure's reason is MacPorts' Error lines from the failing step's own
// part of the log, without its closing lines that point at the log. lint
// says an error and goes on, and the rust run, #35084, quoted lint's line
// for the test step's failure, since the whole log was read. A step that
// says no Error line of its own gives the command's message, never an
// earlier step's line.
func TestAFailuresReasonIsTheFailingSteps(t *testing.T) {
	t.Parallel()
	lint := "LINTSAYS=Error: Line 120 hardcodes /opt/local, use ${prefix} instead"
	results, _ := guestRun(t, twoTargets("declared"), "TESTED=libharbor", "FAIL=test:libharbor", lint)
	require.Equal(t, "passed", results.Targets[0].Outcome)
	require.Equal(t, "failed", results.Targets[0].Tests)
	require.Equal(t, "tests: Failed to test libharbor: it broke", results.Targets[0].Detail)

	results, _ = guestRun(t, twoTargets("declared"), "FAIL=install:libharbor", lint)
	require.Equal(t, "install", results.Targets[0].Phase)
	require.Equal(t, "Failed to install libharbor: it broke", results.Targets[0].Detail)

	results, _ = guestRun(t, twoTargets("declared"), "TESTED=libharbor", "FAIL=test:libharbor", "QUIET=1", lint)
	require.Equal(t, "failed", results.Targets[0].Tests)
	require.Equal(t, "tests: child process exited abnormally", results.Targets[0].Detail, "the command's own message, not lint's line")

	// The command MacPorts says failed, and what the tool said last before
	// it, rather than "command execution failed" (field testing,
	// 2026-10-02: mods' go build).
	results, _ = guestRun(t, twoTargets("declared"), "FAIL=install:libharbor", "COMMAND=1", lint)
	require.Equal(t, "install", results.Targets[0].Phase)
	require.Equal(t, "Failed to build libharbor: go: github.com/spf13/pflag: cannot find package; the command was: cd /work/libharbor && go build ./...", results.Targets[0].Detail)
	require.Empty(t, results.Targets[0].Transient, "the port's own failure")

	// A step the network failed upstream is said, for the host to try the
	// build again: dolt's go build met proxy.golang.org's stream reset
	// (field testing at 23c5e2aa).
	reset := `go: github.com/apache/thrift@v0.24.0: read "https://proxy.golang.org/github.com/apache/thrift/@v/v0.24.0.zip": stream error: stream ID 575; INTERNAL_ERROR; received from peer`
	results, _ = guestRun(t, twoTargets("declared"), "FAIL=install:libharbor", "COMMAND=1", "FAILSAYS="+reset)
	require.Equal(t, "failed", results.Targets[0].Outcome)
	require.Equal(t, reset, results.Targets[0].Transient)
}

// A target an earlier attempt already found blocked is reported blocked
// without being built.
func TestATargetBlockedBeforeIsNotBuilt(t *testing.T) {
	t.Parallel()
	input := twoTargets("declared")
	input.Targets = input.Targets[1:]
	input.Targets[0].Blocked = true
	results, commands := guestRun(t, input)
	require.Equal(t, []guestResult{{ID: "harbor-cli", Outcome: "blocked", Tests: "none", Log: "harbor-cli.log"}}, results.Targets)
	for _, command := range commands {
		require.NotContains(t, command, "subport=", "only the guest's setup ran port")
	}
}

// A guest that can't set up, here a staged tree without its index, says
// so and builds nothing: trouble with the environment, not a result.
func TestAGuestThatCannotSetUpSaysSo(t *testing.T) {
	t.Parallel()
	results, _ := guestRun(t, twoTargets("declared"), "NO_INDEX=1")
	require.Equal(t, "errored", results.State)
	require.Contains(t, results.Detail, "no PortIndex")
	require.Empty(t, results.Targets)
}

// checkoutAt makes a Git checkout where the guest finds a port's
// (CHECKOUTS/<name>), as MacPorts' Git fetch leaves one, and returns the
// commit it is at.
func checkoutAt(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, "checkouts", name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	git := func(args ...string) string {
		command := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		out, err := command.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "release")
	return git("rev-parse", "HEAD")
}

// What a Git-fetched target's fetch checked out is reported with its
// result (batch 20): the commit the check expected, or one it begins with
// where git.branch abbreviates it.
func TestTheGuestReportsTheCommitAGitFetchCheckedOut(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	commit := checkoutAt(t, root, "libharbor")
	cli := checkoutAt(t, root, "harbor-cli")
	input := twoTargets("declared")
	input.Targets[0].Git = &guestGit{Ref: "v4", Expect: commit}
	input.Targets[1].Git = &guestGit{Ref: cli[:8], Expect: cli[:8]}
	results, _ := guestRunIn(t, root, input)
	require.Equal(t, "finished", results.State, results.Detail)
	require.Equal(t, "passed", results.Targets[0].Outcome, results.Targets[0].Detail)
	require.Equal(t, commit, results.Targets[0].Fetched)
	require.Equal(t, "passed", results.Targets[1].Outcome, results.Targets[1].Detail)
	require.Equal(t, cli, results.Targets[1].Fetched, "the whole commit an abbreviation expands to")
}

// An abbreviated commit that's all digits is a commit's beginning, read
// as a string, never a number: one led by a zero, its digits octal's,
// isn't the octal number Tcl's expr would take it for, as 00230075 read
// as 77885 did.
func TestAnAbbreviationOfDigitsIsReadAsWritten(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var commit string
	// A commit whose abbreviation is octal digits led by a zero, found by
	// making commits until one is.
	dir := filepath.Join(root, "checkouts", "libharbor")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	git := func(args ...string) string {
		command := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		out, err := command.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	for i := 0; ; i++ {
		git("commit", "-q", "--allow-empty", "-m", fmt.Sprint("release ", i))
		commit = git("rev-parse", "HEAD")
		if commit[0] == '0' && strings.Trim(commit[:4], "01234567") == "" {
			break
		}
		require.Less(t, i, 20000, "no commit abbreviated as digits")
	}
	input := twoTargets("declared")
	input.Targets = input.Targets[:1]
	input.Targets[0].Git = &guestGit{Ref: commit[:4], Expect: commit[:4]}
	results, _ := guestRunIn(t, root, input)
	require.Equal(t, "finished", results.State, results.Detail)
	require.Equal(t, "passed", results.Targets[0].Outcome, results.Targets[0].Detail)
	require.Equal(t, commit, results.Targets[0].Fetched)
}

// A Git fetch that checked out another commit than the check expected
// fetched another source, one its tag names since the check was planned:
// nothing is built from it, the target fails at fetch saying so, and what
// needs it is blocked.
func TestAGitSourceThatMovedIsNotBuilt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	moved := checkoutAt(t, root, "libharbor")
	expected := strings.Repeat("1", 40)
	input := twoTargets("declared")
	input.Targets[0].Git = &guestGit{Ref: "v4", Expect: expected}
	results, commands := guestRunIn(t, root, input)
	require.Equal(t, "finished", results.State, results.Detail)
	require.Equal(t, "failed", results.Targets[0].Outcome)
	require.Equal(t, "fetch", results.Targets[0].Phase)
	require.Equal(t, moved, results.Targets[0].Fetched, "what it did fetch is reported")
	require.Equal(t, "the source moved: git.branch v4 named "+expected+" when the check was planned, and the fetch checked out "+moved, results.Targets[0].Detail)
	require.Equal(t, "blocked", results.Targets[1].Outcome)
	for _, command := range commands {
		require.False(t, strings.Contains(command, "subport=libharbor") && (strings.Contains(command, " checksum ") || strings.Contains(command, " install ")), "nothing is built from it: %s", command)
	}
}

// A checkout the guest can't read leaves what was fetched unknown: the log
// says so, and the target builds as it did before. A plan that couldn't
// resolve the ref expects nothing, and what was fetched is reported.
func TestAGitFetchThatCantBeReadBuildsAsBefore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	input := twoTargets("declared")
	input.Targets[0].Git = &guestGit{Ref: "v4", Expect: strings.Repeat("1", 40)}
	cli := checkoutAt(t, root, "harbor-cli")
	input.Targets[1].Git = &guestGit{Ref: "v4"}
	results, _ := guestRunIn(t, root, input)
	require.Equal(t, "finished", results.State, results.Detail)
	require.Equal(t, "passed", results.Targets[0].Outcome, results.Targets[0].Detail)
	require.Empty(t, results.Targets[0].Fetched)
	log, err := os.ReadFile(filepath.Join(root, "libharbor.log"))
	require.NoError(t, err)
	require.Contains(t, string(log), "dockhand: which commit the fetch checked out wasn't read:")
	require.Equal(t, "passed", results.Targets[1].Outcome, results.Targets[1].Detail)
	require.Equal(t, cli, results.Targets[1].Fetched)
}

func keep(items []string, wanted func(string) bool) []string {
	var kept []string
	for _, item := range items {
		if wanted(item) {
			kept = append(kept, item)
		}
	}
	return kept
}

// The archives the guest installs targets from are an archive site of
// MacPorts' own kind, one for each type of archive, verified by the key
// MacPorts is told to trust; without them, MacPorts is left as it is.
func TestKeptArchivesAreAnArchiveSiteOfMacPorts(t *testing.T) {
	root := t.TempDir()
	input := twoTargets("declared")
	input.Archives = []guestArchive{{Port: "libharbor", Name: "libharbor-4_0.darwin_25.arm64.tbz2"}, {Port: "zlib", Name: "zlib-1.3.2_0.darwin_25.arm64.tbz2"}}
	input.ArchiveSite, input.ArchiveKeys = "/var/tmp/dockhand-archives", []string{"/var/tmp/dockhand-archives/dockhand.pem", "/var/tmp/dockhand-archives/dockhand.pub"}
	results, _ := guestRunIn(t, root, input)
	require.Equal(t, "finished", results.State, results.Detail)
	etc := filepath.Join(root, "prefix", "etc", "macports")
	keys, err := os.ReadFile(filepath.Join(etc, "pubkeys.conf"))
	require.NoError(t, err)
	require.Equal(t, "/var/tmp/dockhand-archives/dockhand.pem\n/var/tmp/dockhand-archives/dockhand.pub\n", string(keys))
	sites, err := os.ReadFile(filepath.Join(etc, "archive_sites.conf"))
	require.NoError(t, err)
	require.Equal(t, "\nname dockhand_tbz2\nurls file:///var/tmp/dockhand-archives/\ntype tbz2\nprefix "+filepath.Join(root, "prefix")+"\n", string(sites), "one site for the one type")

	plain := t.TempDir()
	results, _ = guestRunIn(t, plain, twoTargets("declared"))
	require.Equal(t, "finished", results.State, results.Detail)
	require.NoFileExists(t, filepath.Join(plain, "prefix", "etc", "macports", "archive_sites.conf"))
	require.NoFileExists(t, filepath.Join(plain, "prefix", "etc", "macports", "pubkeys.conf"))
}

// A build past its bound is ended, with what it started, and said, where
// it ran as long as it would (D16): the target fails at the phase it was
// in, and what depends on it is blocked. Lint has a bound of its own.
func TestABuildPastItsBoundIsEnded(t *testing.T) {
	input := twoTargets("advisory")
	input.BuildTimeout = 1
	started := time.Now()
	results, _ := guestRun(t, input, "HANG=install:libharbor")
	require.Less(t, time.Since(started), 30*time.Second, "the hung build's sleep was ended with it")
	require.Len(t, results.Targets, 2)
	require.Equal(t, "failed", results.Targets[0].Outcome)
	require.Equal(t, "install", results.Targets[0].Phase)
	require.Equal(t, "the build ran past its 1s bound (providers.tart.build_timeout), so it was ended", results.Targets[0].Detail)
	require.Equal(t, "blocked", results.Targets[1].Outcome)

	input = twoTargets("advisory")
	input.LintTimeout = 1
	results, _ = guestRun(t, input, "HANG=lint:libharbor")
	require.Equal(t, "lint", results.Targets[0].Phase)
	require.Equal(t, "lint ran past its 1s bound, so it was ended", results.Targets[0].Detail)
}

// The guest says which target it is building as it begins it, beside the
// results of those it finished, so the target a guest lost midway was
// building is known (decision 44).
func TestTheGuestSaysWhatItIsBuilding(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// harbor-cli's install hangs until its bound ends it, which is time
	// enough to read what the guest says as it builds.
	input := twoTargets("advisory")
	input.BuildTimeout = 2
	command := guestCommand(t, root, input, "HANG=install:harbor-cli")
	require.NoError(t, command.Start())
	var results guestResults
	require.Eventually(t, func() bool {
		var err error
		results, err = readResults(root)
		return err == nil && results.Building == "harbor-cli"
	}, 30*time.Second, 10*time.Millisecond)
	require.Equal(t, "running", results.State)
	require.Len(t, results.Targets, 1)
	require.Equal(t, "libharbor", results.Targets[0].ID)
	require.Eventually(t, func() bool {
		results, _ = readResults(root)
		return results.Phase == "install"
	}, 30*time.Second, 10*time.Millisecond, "and the step of its build under way, for live progress")

	require.NoError(t, command.Wait())
	results, err := readResults(root)
	require.NoError(t, err)
	require.Equal(t, "finished", results.State)
	require.Empty(t, results.Building, "nothing is building once harbor-cli's result is written")
	require.Empty(t, results.Phase)
	require.Len(t, results.Targets, 2)
}

// What a dependency built from source needed to build is deactivated
// before the target builds, so the target sees only its own dependencies
// and what they need at run time: git, built for want of an archive, left
// gettext's msgfmt for taisei, whose check passed where MacPorts CI's
// failed (field testing, 2026-10-07). With nothing outside the closure,
// nothing more is run.
func TestATargetBuildsWithOnlyItsClosureActive(t *testing.T) {
	t.Parallel()
	input := guestInput{Run: "check-1", Attempt: 1, Tests: "declared", Targets: []guestTarget{{ID: "taisei", Name: "taisei", Portfile: "games/taisei/Portfile"}}}
	active := `  git @2.51.0_0 (active)\n  gettext @1.0_0 (active)\n  gettext-runtime @1.0_0 (active)\n  meson @1.9.1_0 (active)\n  python314 @3.14.0_0 (active)\n`
	results, commands := guestRun(t, input, "DEPS=taisei=meson,git", "RDEPS=meson=python314 git=gettext-runtime", "ACTIVE="+active)
	require.Equal(t, "passed", results.Targets[0].Outcome, results.Detail)
	require.Contains(t, commands, "-N -f deactivate gettext", "only what's outside taisei's closure")

	results, commands = guestRun(t, input, "DEPS=taisei=meson,git", "RDEPS=meson=python314 git=gettext-runtime,gettext", "ACTIVE="+active)
	require.Equal(t, "passed", results.Targets[0].Outcome, results.Detail)
	require.False(t, slices.ContainsFunc(commands, func(c string) bool {
		return strings.HasPrefix(c, "-N -f deactivate ") && c != "-N -f deactivate active"
	}), "all of it is the closure's: %v", commands)
}

// Before a target's dependencies install, those with no binary archive for
// the guest are found, with port -b archivefetch, and said in the log: what
// install builds from source, which takes the time (the roadmap's part 3).
func TestTheGuestSaysWhichDependenciesBuildFromSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	results, commands := guestRunIn(t, root, twoTargets("declared"), "DEPS=libharbor=zlib,rust harbor-cli=libharbor", "NOARCHIVE=rust")
	require.Equal(t, "finished", results.State, results.Detail)
	require.Contains(t, commands, "-N -b archivefetch zlib")
	require.Contains(t, commands, "-N -b archivefetch rust")
	log, err := os.ReadFile(filepath.Join(root, "libharbor.log"))
	require.NoError(t, err)
	require.Contains(t, string(log), "dockhand: no binary archive for rust on this release; install builds it from source\n")
	require.Empty(t, results.FromSource, "cleared once the target's result is written")
	// Where something builds from source, the install says what it does,
	// for a compiler's error and MacPorts' "Command failed:" line; where
	// nothing does, it's quiet (the exiv2 field notes, 2026-10-19).
	require.Contains(t, commands, "-N -d install --unrequested zlib rust")
}

// A dependency's failure is the dependency's, at a phase of its own, with
// MacPorts' line naming it: it read as the target's install failing
// (the exiv2 session's notes, 2026-10-21: luminance-hdr, where
// qt5-qtwebengine had failed to build).
func TestADependencysFailureIsItsOwnPhase(t *testing.T) {
	t.Parallel()
	results, _ := guestRun(t, twoTargets("declared"), "DEPS=libharbor=qt5-qtwebengine", "DEPFAILS=qt5-qtwebengine")
	require.Equal(t, "failed", results.Targets[0].Outcome)
	require.Equal(t, "dependencies", results.Targets[0].Phase)
	require.Contains(t, results.Targets[0].Detail, "Failed to build qt5-qtwebengine: command execution failed")
	require.Empty(t, results.Targets[0].DependencyVersion, "where the tree doesn't say, none")

	// It names the dependency, and its version and revision as the tree
	// has them, for an acceptance that stands until either changes.
	results, commands := guestRun(t, twoTargets("declared"), "DEPS=libharbor=libgcc15", "DEPFAILS=libgcc15", "DEPVERSION=libgcc15=15.2.0,1")
	require.Equal(t, "libgcc15", results.Targets[0].Dependency)
	require.Equal(t, "15.2.0_1", results.Targets[0].DependencyVersion)
	require.Contains(t, commands, "-q info --line --version --revision libgcc15")
}

// A passed target says what its installed Mach-O files link under the
// prefix, each library with the port that provides it, an @rpath link
// resolved through the binary's LC_RPATH; a file that isn't a Mach-O, a
// system library, and the library's own install name are left out
// (v0.5.0 part 6). Where its files can't be listed, the links are absent,
// unknown, never none.
func TestTheGuestSaysWhatAPassedTargetsBinariesLink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	prefix := filepath.Join(root, "prefix")
	bin, lib := filepath.Join(prefix, "bin"), filepath.Join(prefix, "lib")
	require.NoError(t, os.MkdirAll(lib, 0o755))
	require.NoError(t, os.MkdirAll(bin, 0o755))
	harbor, own, script := filepath.Join(bin, "harbor"), filepath.Join(lib, "libharbor.1.dylib"), filepath.Join(bin, "harbor-notes")
	onig, z := filepath.Join(lib, "libonig.5.dylib"), filepath.Join(lib, "libz.1.dylib")
	macho := []byte{0xcf, 0xfa, 0xed, 0xfe, 0, 0, 0, 0}
	for _, file := range []string{harbor, own, onig, z} {
		require.NoError(t, os.WriteFile(file, macho, 0o755))
	}
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(harbor+".links", []byte(harbor+":\n\t@rpath/libonig.5.dylib (compatibility version 6.0.0, current version 6.9.0)\n\t"+own+" (compatibility version 1.0.0, current version 1.0.0)\n\t/usr/lib/libSystem.B.dylib (compatibility version 1.0.0, current version 1351.0.0)\n"), 0o644))
	require.NoError(t, os.WriteFile(harbor+".loads", []byte("Load command 12\n          cmd LC_RPATH\n      cmdsize 32\n         path @loader_path/../lib (offset 12)\n"), 0o644))
	require.NoError(t, os.WriteFile(own+".links", []byte(own+":\n\t"+own+" (compatibility version 1.0.0, current version 1.0.0)\n\t"+z+" (compatibility version 1.0.0, current version 1.3.1)\n"), 0o644))
	require.NoError(t, os.WriteFile(own+".id", []byte(own+":\n"+own+"\n"), 0o644))
	input := guestInput{Run: "check-1", Attempt: 1, Tests: "declared", Targets: []guestTarget{{ID: "harbor", Name: "harbor", Portfile: "devel/harbor/Portfile", Log: buildenv.LogName("harbor")}}}
	results, _ := guestRunIn(t, root, input, "CONTENTS="+harbor+"\\n"+own+"\\n"+script+"\\n", "PROVIDES="+onig+"=oniguruma "+own+"=harbor")
	require.Equal(t, "finished", results.State, results.Detail)
	require.Equal(t, []guestLink{
		{Binary: harbor, Library: onig, Port: "oniguruma"},
		{Binary: harbor, Library: own, Port: "harbor"},
		{Binary: own, Library: z},
	}, results.Targets[0].Links)

	results, _ = guestRun(t, input)
	require.Equal(t, "finished", results.State, results.Detail)
	require.Nil(t, results.Targets[0].Links, "its files couldn't be listed")
}
