package eval

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

// admission is the one table of exec admission (#39's first item, taken
// with #110): each command line, what the Mac's own context does with it,
// the dispatcher running it or refusing it with the reason, and the same
// judgment of its source text (macports.CommandLineRefusal), which reads a
// Portfile's exec without running it. In a modelled context no program
// runs: every row, admitted or not, is answered "couldn't execute" and
// marked closed, but for what the facts table answers itself, xcrun's SDK
// path. Writes, the network, and Base's own process commands are
// refused in both (TestTheDispatcherRefusesEffects).
var admission = []struct {
	line, refused string
	table         bool
}{
	{"/usr/libexec/java_home -V", "", false},
	{"-ignorestderr xcrun --sdk macosx --show-sdk-path 2> /dev/null", "", true},
	{"env DEVELOPER_DIR=/Library/Developer/CommandLineTools xcrun --find clang", "", false},
	{"uname -m", "", false},
	{"env LC_ALL=C uname -m 2>@1", "", false},
	{"sysctl -n hw.ncpu", "", false},
	{"sw_vers -productVersion", "", false},
	{"-ignorestderr echo hi 2>/dev/null", "", false},
	// A pipeline whose writer writes nothing: true exits without reading,
	// and echo, writing to it, now and then found no reader (the M1's
	// rerun, A0: "child killed: write on pipe with no readers").
	{"/usr/bin/true | /usr/bin/true", "", false},
	{"/usr/libexec/java_home --exec /usr/bin/true", "runs java_home with --exec", false},
	{"xcrun --sdk macosx clang", "runs xcrun with clang", false},
	{"git config user.name x", "runs git config", false},
	{"git log --output=x", "runs git with --output=x", false},
	{"echo hi > /dev/stdout", "writes /dev/stdout", false},
	{"echo hi | /usr/bin/tee /dev/null", "runs tee, which is not known to only report", false},
	{"env A=1 /usr/bin/touch x", "runs touch, which is not known to only report", false},
	{"/bin/sh -c true", "runs sh, which is not known to only report", false},
	{"/usr/bin/true &", "leaves a process running", false},
}

func TestExecAdmissionIsOneTable(t *testing.T) {
	t.Parallel()
	for _, row := range admission {
		t.Run(row.line, func(t *testing.T) {
			t.Parallel()
			var words []macports.Argument
			for _, word := range strings.Fields(row.line) {
				words = append(words, macports.Argument{Text: word, Literal: true})
			}
			require.Equal(t, row.refused, macports.CommandLineRefusal(words), "the source text's judgment")

			// Caught, as a program that runs and fails, java_home with no
			// JDK, is; a refusal is recorded whether caught or not.
			_, err := evaluateLines(t, "catch {exec "+row.line+"}", false)
			if row.refused == "" {
				require.NoError(t, err, "the Mac's own context admits it")
			} else {
				require.ErrorIs(t, err, macports.ErrRefused)
				require.ErrorContains(t, err, row.refused)
			}

			got, port := observedIn(t, monterey, nil, "lappend results [catch {exec "+row.line+"} said]")
			if row.table {
				require.Equal(t, []string{"0"}, got, "the table answers")
				return
			}
			require.Equal(t, []string{"1"}, got, "nothing runs in a modelled context")
			require.Positive(t, closedCalls(port)["exec"], "%v", port.Ledger)
		})
	}
}
