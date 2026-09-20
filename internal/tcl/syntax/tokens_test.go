package syntax

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTokenRunsReachWordsTablesQuotesAndSubstitutions(t *testing.T) {
	t.Parallel()
	src := []byte("# 1.0 in a comment\n" +
		"version 1.0\n" +
		"set modules {\n    qtbase {{abc def} 3}\n    # not a comment to Tcl, but skipped as one\n}\n" +
		"subport foo { revision 3 }\n" +
		"notes \"revision 3\"\n" +
		"set x [lindex $y 0]\n" +
		"bad {say \"hi}\n")
	script, errs := Parse(src)
	require.Empty(t, errs)
	var runs [][]string
	for _, run := range script.TokenRuns(src) {
		var words []string
		for _, token := range run {
			words = append(words, token.Text(src))
		}
		runs = append(runs, words)
	}
	require.Equal(t, [][]string{
		{"version", "1.0"},
		{"abc", "def"},
		{"3"},
		{"qtbase"},
		{"set", "modules"},
		{"revision", "3"},
		{"subport", "foo"},
		{"notes", "revision 3"},
		{"lindex", "$y", "0"},
		{"set", "x", "[lindex $y 0]"},
		{"bad"},
	}, runs, "a braced word is replaced by its nested runs, which come before the run that held it; comments, and a body that neither parses nor splits, contribute nothing")
}
