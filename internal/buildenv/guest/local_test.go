package guest

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// asIs is two targets checked in a prefix of the person's, as the local
// provider asks: among whatever is active, put back after.
func asIs(local Local) Input {
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
	results, commands := guestRunIn(t, root, asIs(Local{}), active, "DEPS=libharbor=zlib harbor-cli=libharbor", "FOREIGN="+homebrew)
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
	results, commands := guestRun(t, asIs(Local{Keep: true}), active, "NEWVERSION=4_0")
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
	results, commands := guestRun(t, asIs(Local{Clean: true}), active, "DEPS=libharbor=zlib harbor-cli=libharbor")
	require.Equal(t, "finished", results.State, results.Detail)
	require.Contains(t, commands, "-N -f deactivate active")
	require.Equal(t, "yes", results.Environment["restored"])
	got := readRegistry(t, path)
	require.Contains(t, got, "  zlib @1.3.2_0 (active)\n")
	require.Contains(t, got, "  xz @5.8.1_0 (active)\n")
	require.NotContains(t, got, "libharbor")
}

// An image of the version the branch builds, installed already, would be
// activated rather than built. One inactive and unrequested, as a check's
// put-back leaves it, is uninstalled and said, and the branch's builds;
// an active or requested one is the person's: the check stops, saying so,
// and puts the prefix back (rc7's acceptance).
func TestALocalCheckMakesWayForAnImageItsPutBackLeft(t *testing.T) {
	t.Parallel()
	_, active := registry(t, "  zlib @1.3.2_0 (active)\n")
	results, commands := guestRun(t, asIs(Local{}), active, "DEPVERSION=libharbor=4,0", "INSTALLED=  libharbor @4_0+doc\\n  libharbor @3_0\\n")
	require.Equal(t, "finished", results.State, results.Detail)
	require.Contains(t, commands, "-N uninstall libharbor @4_0+doc")
	require.False(t, slices.ContainsFunc(commands, func(c string) bool { return strings.Contains(c, "uninstall libharbor @3_0") }), "another version stays")
	require.True(t, slices.ContainsFunc(commands, func(c string) bool { return strings.Contains(c, "-dkns install") }), "the branch's builds")
	require.Equal(t, "yes", results.Environment["restored"])
}

func TestALocalCheckRefusesWhereThePersonsImageOfTheVersionIsInstalled(t *testing.T) {
	t.Parallel()
	for name, env := range map[string][]string{
		"active":    {"INSTALLED=  libharbor @4_0 (active)\\n"},
		"requested": {"INSTALLED=  libharbor @4_0\\n", "REQUESTED=  libharbor @4_0\\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, active := registry(t, "  zlib @1.3.2_0 (active)\n")
			results, commands := guestRun(t, asIs(Local{}), append([]string{active, "DEPVERSION=libharbor=4,0"}, env...)...)
			require.Equal(t, "refused", results.State)
			require.Contains(t, results.Detail, "libharbor @4_0 is installed in")
			require.Contains(t, results.Detail, "port uninstall libharbor @4_0")
			require.Equal(t, "yes", results.Environment["restored"])
			require.False(t, slices.ContainsFunc(commands, func(c string) bool { return strings.Contains(c, "uninstall") }), "the person's image stays")
			require.False(t, slices.ContainsFunc(commands, func(c string) bool { return strings.Contains(c, "-dkns install") }), "nothing is built")
		})
	}
}

// A check whose process ended before it put the prefix back is put back
// by the program run to do only that, from what was active before.
func TestTheGuestPutsBackWhatAnEarlierCheckLeft(t *testing.T) {
	t.Parallel()
	path, active := registry(t, "  libharbor @4_0 (active)\n", "  harbor-cli @4_0 (active)\n")
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "before.txt"), []byte("  libharbor @3_0 (active)\n  zlib @1.3.2_0 (active)\n"), 0o644))
	results, commands := guestRunIn(t, root, asIs(Local{Restore: true}), active)
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

// Run under sudo, which passes no environment of the person's, the
// program takes the check's directory as its argument and sets MacPorts'
// PORTSRC from it itself, so one sudoers line for the prefix's port-tclsh
// lets it run (the person's sitting on rc8, 2026-10-29).
func TestALocalCheckTakesItsDirectoryAsItsArgument(t *testing.T) {
	t.Parallel()
	_, active := registry(t, "  zlib @1.3.2_0 (active)\n")
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "sources.conf"), []byte("file://"+root+"/ports [default]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "macports.conf"), []byte("sources_conf "+filepath.Join(root, "sources.conf")+"\n"), 0o644))
	// The fake port keeps its own files where DOCKHAND_GUEST_ROOT says,
	// which the program no longer reads given its argument.
	command := guestCommand(t, root, asIs(Local{}), active, "DEPS=", "DOCKHAND_GUEST_ROOT="+t.TempDir(), "PORTSRC=")
	command.Args = append(command.Args, root)
	output, _ := command.CombinedOutput()
	results, err := readResults(root)
	require.NoError(t, err, "%s", output)
	require.Equal(t, "finished", results.State, results.Detail)
	conf, err := os.ReadFile(filepath.Join(root, "port.log.conf"))
	require.NoError(t, err, "port read the check's macports.conf through PORTSRC")
	require.Contains(t, string(conf), "file://"+root+"/ports [default]")
}
