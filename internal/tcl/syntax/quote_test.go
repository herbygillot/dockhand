package syntax

import (
	"encoding/hex"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testsupport"
)

// wordValues are values a word must carry exactly: punctuation, braces
// balanced and not, backslashes where braces can't hold them, whitespace,
// and Unicode. (The private-helper review of 2026-09-28, finding 5.)
var wordValues = []string{
	"", "plain", "two words", "Tool {x}", `Tool C:\temp`, "unbalanced } brace", "{", "}", "{a} {b}", "{}", "} {", `\{}`,
	`ends with \`, "back\\\nslash newline", `\{escaped brace`, "$var [cmd] ;semi \"quoted\"",
	"#hash", "a#b", "tab\there", "new\nline", "ünïcödé ✓",
}

// A value written as a word reads back as itself, as a list's one element.
func TestAWordReadsBackAsItsValue(t *testing.T) {
	for _, value := range wordValues {
		word := Quote(value)
		got, errs := ListValues(word)
		require.Empty(t, errs, "%q as %s", value, word)
		require.Equal(t, []string{value}, got, "%q as %s", value, word)
	}
	require.Equal(t, "plain", Quote("plain"), "nothing to quote")
	require.Equal(t, "{Tool {x}}", Quote("Tool {x}"), "braced where the braces balance")
	require.Equal(t, `{Tool C:\temp}`, Quote(`Tool C:\temp`), "a backslash is itself inside braces")
	require.Equal(t, `unbalanced\ \}\ brace`, Quote("unbalanced } brace"), "backslashed where braces can't hold it")
	require.Equal(t, "{#hash}", Quote("#hash"), "a leading # is quoted")
}

// Tcl reads each word as its value, as an argument of a command.
func TestTclReadsAWordAsItsValue(t *testing.T) {
	tclsh := testsupport.MacPortsTclsh(t)
	var script strings.Builder
	for _, value := range wordValues {
		script.WriteString("puts [binary encode hex [encoding convertto utf-8 " + Quote(value) + "]]\n")
	}
	command := exec.CommandContext(t.Context(), tclsh)
	command.Stdin = strings.NewReader(script.String())
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	lines := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	require.Len(t, lines, len(wordValues))
	for i, value := range wordValues {
		require.Equal(t, hex.EncodeToString([]byte(value)), lines[i], "%q as %s", value, Quote(value))
	}
}
