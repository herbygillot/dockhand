package eval

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// evaluateLines evaluates a port whose Portfile runs lines after its name
// and version, observed or not.
func evaluateLines(t *testing.T, lines string, observed bool) (macports.Observation, error) {
	t.Helper()
	e := liveEvaluator(t)
	tree := fixtureTree(t)
	putFile(t, tree.Root(), "devel/effects/Portfile", "PortSystem 1.0\nname effects\nversion 1\n"+lines+"\n")
	// The target is named, not resolved: resolving evaluates the port,
	// which a refusal would stop first.
	bound, err := tree.Select(model.Target{Name: "effects", Portfile: "devel/effects/Portfile"})
	require.NoError(t, err)
	if observed {
		return e.Observe(t.Context(), bound, macports.ObservationRequest{Declarations: true})
	}
	snapshot, err := e.Evaluate(t.Context(), bound)
	return macports.Observation{Snapshot: snapshot}, err
}

func TestTheDispatcherRefusesEffects(t *testing.T) {
	t.Parallel()
	scratch := t.TempDir()
	existing := filepath.Join(scratch, "existing")
	require.NoError(t, os.WriteFile(existing, []byte("kept\n"), 0600))
	created := filepath.Join(scratch, "created")
	for _, c := range []struct {
		name, lines, reason string
	}{
		{"a directory made", "file mkdir " + created, "changes the filesystem"},
		{"a file deleted, the refusal caught", "catch {file delete " + existing + "}", "changes the filesystem"},
		{"a file opened for writing", "set f [open " + created + " w]", "opens " + created + " for writing"},
		{"a file opened with flags that create it", "set f [open " + created + " {WRONLY CREAT}]", "for writing"},
		{"a file's times changed", "file mtime " + existing + " 0", "changes a file's times"},
		{"a link made", "file link -symbolic " + created + " " + existing, "changes the filesystem"},
		{"output redirected to a file", "exec echo hi > " + created, "writes " + created},
		{"a program not listed", "exec /bin/sh -c {touch " + created + "}", "runs sh, which is not known to only report"},
		{"a program in a pipeline not listed", "exec echo hi | /usr/bin/tee " + created, "runs tee"},
		{"a pipe opened to a program not listed", "set f [open |[list /usr/bin/touch " + created + "]]", "runs touch"},
		{"a listed program in a form that acts", "exec /usr/libexec/java_home --exec /usr/bin/touch " + created, "runs java_home with --exec"},
		{"a git subcommand that writes", "exec git -C " + scratch + " config user.name x", "runs git config"},
		{"xcrun running a tool", "exec xcrun --sdk macosx touch " + created, "runs xcrun with touch"},
		{"env running a program not listed", "exec env A=1 /usr/bin/touch " + created, "runs touch"},
		{"a process left running", "exec /usr/bin/true &", "leaves a process running"},
		{"the network", "catch {socket 127.0.0.1 9}", "reaches the network"},
		{"a shell command through Base", "catch {system {touch " + created + "}}", "runs a process outside the dispatcher"},
		{"the registry written", "catch {registry_new effects 1 0 {} 0}", "changes the MacPorts registry"},
	} {
		for _, observed := range []bool{false, true} {
			_, err := evaluateLines(t, c.lines, observed)
			require.ErrorIs(t, err, macports.ErrRefused, "%s, observed %v", c.name, observed)
			require.ErrorContains(t, err, c.reason, "%s, observed %v", c.name, observed)
			require.ErrorContains(t, err, "Portfile:4", "%s: where it was attempted", c.name)
			require.NoFileExists(t, created, c.name)
			content, readErr := os.ReadFile(existing)
			require.NoError(t, readErr, c.name)
			require.Equal(t, "kept\n", string(content), c.name)
		}
	}
}

func TestTheDispatcherAdmitsProgramsThatOnlyReport(t *testing.T) {
	t.Parallel()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(os.DevNull, link))
	for _, line := range []string{
		"set target [file link " + link + "]",
		"set machine [exec uname -m]",
		"set cpus [exec sysctl -n hw.ncpu]",
		"set said [exec -ignorestderr echo hi 2>/dev/null]",
		"set machine [exec env LC_ALL=C uname -m 2>@1]",
		// A pipeline whose writer writes nothing: true exits without
		// reading, and echo, writing to it, now and then found no reader
		// (the M1's rerun, A0: "child killed: write on pipe with no
		// readers"). No admitted program reads its input to drain one.
		"set read [exec /usr/bin/true | /usr/bin/true]",
		"set version [exec sw_vers -productVersion]",
		"set f [open " + os.DevNull + " r]; close $f",
		"set found [file exists /usr/bin/true]",
		"if {[catch {exec " + filepath.Join(t.TempDir(), "ruby1.8") + " -e {puts 1}}]} { set fallback 1 }",
	} {
		for _, observed := range []bool{false, true} {
			_, err := evaluateLines(t, line, observed)
			require.NoError(t, err, "%s, observed %v", line, observed)
		}
	}
}
