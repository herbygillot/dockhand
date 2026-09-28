package tart

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// fakePort stands for MacPorts' port in a guest: it records each command,
// has the ports ACTIVE lists active ("  name @spec (active)" lines), prints
// each target's dependencies from DEPS ("name=dep ..."), and fails the
// phases FAIL names ("phase:port ..."). A port's archive is ARCHIVES/<name>,
// and its directory devel/<name>, but for UNRESOLVED, which port can't
// resolve.
const fakePort = `#!/bin/sh
echo "$*" >> "$PORT_LOG"
case "$*" in
  *"-q installed active"*) printf '%b' "$ACTIVE"; exit 0 ;;
  *"-q installed"*) exit 0 ;;
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
      case "$*" in *"depof:${entry%%=*}") echo "${entry#*=}" ;; esac
    done
    exit 0 ;;
esac
for entry in $FAIL; do
  phase=${entry%%:*}; port=${entry#*:}
  case "$*" in
    *" $phase "*"subport=$port"*|*" $phase "*"subport=$port "*) echo "Error: Failed to $phase $port: it broke"; exit 1 ;;
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
	data, err := json.Marshal(input)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "input.json"), data, 0o644))
	// MacPorts itself is stood in for: the platform, and which ports
	// declare tests.
	prelude := `
package provide macports 1.0
namespace eval macports {variable os_platform darwin; variable os_major 25; variable build_arch arm64}
proc mportinit {} {}
proc declares_tests {portdir name variants} { return [expr {$name in $::env(TESTED)}] }
set foreignManagers {}
`
	script := filepath.Join(root, "guest.tcl")
	require.NoError(t, os.WriteFile(script, append([]byte(prelude), guestProgram...), 0o644))
	command := exec.CommandContext(t.Context(), executable, script)
	portLog := filepath.Join(root, "port.log")
	command.Env = append(append(os.Environ(), "DOCKHAND_GUEST_ROOT="+root, "PORT_LOG="+portLog, "TESTED=", "DEPS=", "FAIL=", "ACTIVE=", "ARCHIVES="+root, "UNRESOLVED="), env...)
	output, _ := command.CombinedOutput()
	data, err = os.ReadFile(filepath.Join(root, "results.json"))
	require.NoError(t, err, "%s", output)
	var results guestResults
	require.NoError(t, json.Unmarshal(data, &results), "%s", data)
	commands, _ := os.ReadFile(portLog)
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(commands)), "\n") {
		if line != "" {
			// The port directory is the scratch root's; the tail is enough.
			lines = append(lines, strings.ReplaceAll(line, filepath.Join(root, "ports")+"/", ""))
		}
	}
	return results, lines
}

func twoTargets(tests string) guestInput {
	return guestInput{Run: "check-1", Attempt: 1, Tests: tests, Targets: []guestTarget{
		{ID: "libharbor", Name: "libharbor", Portfile: "devel/libharbor/Portfile"},
		{ID: "harbor-cli", Name: "harbor-cli", Portfile: "devel/harbor-cli/Portfile", DependsOn: []string{"libharbor"}},
	}}
}

// Each target is linted, its dependencies installed, and then fetched,
// checksummed, and installed, as MacPorts CI does, with its declared tests
// after; the results say so target by target.
func TestTheGuestBuildsEachTargetInCIsOrder(t *testing.T) {
	t.Parallel()
	results, commands := guestRun(t, twoTargets("declared"), "TESTED=libharbor", "DEPS=libharbor=zlib harbor-cli=libharbor")
	require.Equal(t, "finished", results.State, results.Detail)
	require.Equal(t, []guestResult{
		{ID: "libharbor", Outcome: "passed", Tests: "passed", Log: "target-1.log", Active: []guestPort{}},
		{ID: "harbor-cli", Outcome: "passed", Tests: "none", Log: "target-2.log", Active: []guestPort{}},
	}, results.Targets)
	require.Equal(t, "arm64", results.Environment["architecture"])
	var libharbor []string
	for _, command := range commands {
		if strings.Contains(command, "devel/libharbor") || strings.Contains(command, "depof:libharbor") {
			libharbor = append(libharbor, command)
		}
	}
	require.Equal(t, []string{
		"-N -D devel/libharbor lint subport=libharbor",
		"-q echo depof:libharbor",
		"-N -D devel/libharbor -d fetch subport=libharbor",
		"-N -D devel/libharbor -d checksum subport=libharbor",
		"-N -D devel/libharbor -dkn install --unrequested subport=libharbor",
		"-N -D devel/libharbor -d test subport=libharbor",
	}, keep(libharbor, func(c string) bool { return !strings.Contains(c, "installed") }))
	require.Contains(t, commands, "-N -d install --unrequested zlib", "a target's dependencies are installed first")
	require.Contains(t, commands, "-N -d install --unrequested libharbor", "the dependent's changed dependency is among them")
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
		{Name: "zlib", Spec: "@1.3.2_0", Directory: "devel/zlib", Archive: digest("zlib's archive")},
		{Name: "xz", Spec: "@5.8.1_0+universal", Directory: "devel/xz"},
		{Name: "gone", Spec: "@1.0_0"},
	}, results.Targets[0].Active)
	require.Equal(t, digest("libharbor's archive"), results.Targets[0].Archive)
	require.Equal(t, filepath.Join(archives, "libharbor"), results.Targets[0].ArchiveFile, "where it is, for the host to keep it")
	require.Contains(t, commands, "-q location zlib @1.3.2_0 xz @5.8.1_0+universal gone @1.0_0", "asked once for all of them")
	require.Contains(t, commands, "-q dir gone", "asked alone once the list failed")
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

// A target an earlier attempt already found blocked is reported blocked
// without being built.
func TestATargetBlockedBeforeIsNotBuilt(t *testing.T) {
	t.Parallel()
	input := twoTargets("declared")
	input.Targets = input.Targets[1:]
	input.Targets[0].Blocked = true
	results, commands := guestRun(t, input)
	require.Equal(t, []guestResult{{ID: "harbor-cli", Outcome: "blocked", Tests: "none", Log: "target-1.log"}}, results.Targets)
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
