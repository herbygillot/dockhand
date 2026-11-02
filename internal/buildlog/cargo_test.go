package buildlog

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The crates a cargo build compiled are read from its "Compiling" lines,
// each once, between the lines a step began and the next did, MacPorts'
// own log's prefix allowed; a log with none names none.
func TestTheCratesACargoBuildCompiledAreRead(t *testing.T) {
	log := strings.Join([]string{
		"--->  Building omnidiff",
		"   Compiling libc v0.2.170",
		"   Compiling libz-sys v1.1.22",
		":info:build    Compiling ratatui v0.29.0",
		"   Compiling libc v0.2.170",
		"     Running `rustc --crate-name libz_sys ...`",
		"   Compiling omnidiff v0.2.0 (/opt/local/var/macports/build/omnidiff/work/omnidiff-0.2.0)",
		"warning: Compiling nothing v1 is not a line cargo prints this way",
		"--->  Testing omnidiff",
		"   Compiling tempfile v3.20.0",
	}, "\n") + "\n"
	require.Equal(t, []string{"libc", "libz-sys", "ratatui", "omnidiff"}, Compiled(strings.NewReader(log), 2, 9))
	require.Equal(t, []string{"libz-sys", "ratatui", "libc", "omnidiff", "tempfile"}, Compiled(strings.NewReader(log), 3, 0))
	require.Empty(t, Compiled(strings.NewReader("--->  Building jq\nmake all\n"), 1, 0))
}
