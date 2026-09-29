package buildlog

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A failed build's likely cause is its first compiler error, as clang
// writes its diagnostics: beekeeper-studio's node-gyp rebuild of
// sqlanywhere, which MacPorts summed up as make's exit code (the
// beekeeper-studio run's finding 2, D10).
func TestTheFirstCompilerErrorIsTheLikelyCause(t *testing.T) {
	log := strings.Join([]string{
		"--->  Building beekeeper-studio",
		"In file included from ../src/sqlanywhere.cpp:4:",
		"../src/h/sqlany_utils.h:12:10: warning: 'register' storage class specifier is deprecated [-Wdeprecated-register]",
		"../src/h/sqlany_utils.h:12:10: note: expanded from here",
		"gyp ERR! build error",
		"../src/h/sqlany_utils.h:14:10: fatal error: 'source_location' file not found",
		"   14 | #include <source_location>",
		"../src/sqlanywhere.cpp:20:3: error: use of undeclared identifier 'SQLAny'",
		"2 errors generated.",
		"make: *** [Release/obj.target/sqlanywhere/src/sqlanywhere.o] Error 1",
		"Error: Failed to build beekeeper-studio: command execution failed",
	}, "\n")
	cause, ok := First(strings.NewReader(log))
	require.True(t, ok)
	require.Equal(t, Cause{Line: "../src/h/sqlany_utils.h:14:10: fatal error: 'source_location' file not found", Number: 6}, cause)

	cause, ok = First(strings.NewReader("main.c:3: error: expected ';' before '}' token\r\n"))
	require.True(t, ok, "GCC's, without a column")
	require.Equal(t, "main.c:3: error: expected ';' before '}' token", cause.Line)

	long := strings.Repeat("x", 200<<10)
	cause, ok = First(strings.NewReader(long + "\nfoo.swift:1:2: error: cannot find 'bar' in scope\n"))
	require.True(t, ok, "past a line longer than the scanner's first buffer")
	require.Equal(t, 2, cause.Number)

	for _, none := range []string{
		"make: *** [all] Error 2\nError: Failed to build jq: command execution failed\n",
		"foo.c:1:2: warning: unused variable 'x'\n",
		"error: something without a place\n",
		"12:34:56: error: a time isn't a file\n",
		"",
	} {
		_, ok := First(strings.NewReader(none))
		require.False(t, ok, none)
	}
}
