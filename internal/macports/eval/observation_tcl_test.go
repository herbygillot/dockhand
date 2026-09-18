package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/stretchr/testify/require"
)

// tclSession starts the evaluator's interpreter on the fixture tree, with
// every embedded script loaded, and returns a way to evaluate Tcl in it.
func tclSession(t *testing.T) (macports.Tree, func(script string) string) {
	t.Helper()
	evaluator := liveEvaluator(t)
	tree := fixtureTree(t)
	session, _, err := evaluator.start(t.Context(), tree)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return tree, func(script string) string {
		t.Helper()
		reply, err := session.Call(t.Context(), "eval", script)
		require.NoError(t, err, script)
		return reply
	}
}

// observedWorker is a plain child interpreter set up the way observe_worker
// sets up a MacPorts worker: the recording namespace, the platform script,
// and the source root, with the option commands the trace attaches to
// stubbed. Sourcing a file named Portfile in it is what makes its reads
// owned, as a Portfile's reads are.
func observedWorker(t *testing.T, tcl func(string) string) {
	t.Helper()
	tcl(`interp create w
w eval {
    proc PortSystem {version} {}
    foreach name {checksums checksums-append checksums-prepend revision} { proc $name args {} }
    proc option {name} { return "" }
}
set ::dockhand::operands {}
::dockhand::observe_worker {::macports::worker_init w} 0 {} leave`)
}

// The observation trace records a Portfile's reads of host state with the
// frames that made them, classifies what the read reaches, and ignores
// reads made outside a Portfile or that cannot reach host state.
func TestObservationTraceRecordsPortfileHostReadsWithFrames(t *testing.T) {
	t.Parallel()
	tree, tcl := tclSession(t)
	observedWorker(t, tcl)
	root := tree.Root()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "devel", "probe", "files"), 0o755))
	inside := filepath.Join(root, "devel", "probe", "files", "note")
	require.NoError(t, os.WriteFile(inside, []byte("captured\n"), 0o644))
	link := filepath.Join(root, "devel", "probe", "files", "elsewhere")
	require.NoError(t, os.Symlink(t.TempDir(), link))
	portfile := filepath.Join(root, "devel", "probe", "Portfile")
	require.NoError(t, os.WriteFile(portfile, []byte(`PortSystem 1.0
file exists /etc/hosts
file exists `+inside+`
file exists `+link+`
file join /etc hosts
exec /bin/echo probed
glob -nocomplain /etc/host*
checksums sha256 abcd
`), 0o644))
	tcl("w eval [list source " + portfile + "]")
	tcl("w eval {file exists /etc/hosts}")
	events, errs := syntax.ListValues(tcl("w eval {set ::dockhand_observation::events}"))
	require.Empty(t, errs)
	var problems, commands []string
	for _, event := range events {
		parts, errs := syntax.ListValues(event)
		require.Empty(t, errs)
		require.Len(t, parts, 2, event)
		command, errs := syntax.ListValues(parts[0])
		require.Empty(t, errs)
		commands = append(commands, command[0])
		if command[0] == "dockhand.host-access" {
			problems = append(problems, command[1])
			frames, errs := syntax.ListValues(parts[1])
			require.Empty(t, errs)
			var files []string
			for _, frame := range frames {
				fields, errs := syntax.ListValues(frame)
				require.Empty(t, errs)
				require.Len(t, fields, 3, frame)
				files = append(files, fields[0])
			}
			require.Contains(t, strings.Join(files, "\n"), portfile, "a recorded read carries the Portfile frame that made it")
		}
	}
	require.Equal(t, []string{
		"modeled context depends on filesystem state outside the captured ports tree",
		"modeled context depends on a symbolic link while evaluating the Portfile",
		"modeled context depends on a host process executed while evaluating the Portfile",
		"modeled context depends on directory enumeration while evaluating the Portfile",
	}, problems, "the read inside the tree and the file join are not host accesses, and the read made outside the Portfile is not recorded")
	require.Contains(t, commands, "checksums", "option declarations are recorded beside host accesses")
	require.Equal(t, 5, len(commands))
}

// A modeled platform is validated before the interpreter is overridden, and
// an unsupported one is refused without changing anything.
func TestObservationSetupRefusesUnsupportedPlatforms(t *testing.T) {
	t.Parallel()
	_, tcl := tclSession(t)
	for _, platform := range []string{"{plan9 20 arm64 11}", "{darwin 7 x86_64 10.3}", "{darwin 20 mips 11}", "{darwin twenty arm64 11}"} {
		reply := tcl("catch {::dockhand::observation_setup " + platform + " 0 {}} message; set message")
		require.Equal(t, "unsupported modeled platform", reply, platform)
	}
	require.Equal(t, "0", tcl("set ::dockhand::modeled"), "a refused setup leaves the session unmodeled")
	tcl("::dockhand::observation_setup {darwin 21 x86_64 12} 1 {}")
	require.Equal(t, "1", tcl("set ::dockhand::modeled"))
	require.Equal(t, "21", tcl("set ::macports::os_major"))
	require.Equal(t, "x86_64", tcl("set ::macports::build_arch"))
	require.Equal(t, "12.0", tcl("set ::macports::macosx_deployment_target"))
	require.Equal(t, "arm64 x86_64", tcl("set ::macports::universal_archs"))
}
