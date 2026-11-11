package cmdtest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// atWidth runs a command line with COLUMNS at a width, its standard
// output.
func atWidth(t *testing.T, columns string, args ...string) string {
	t.Helper()
	Setenv(t, "COLUMNS", columns)
	out, errs, err := Dockhand(t, args...)
	require.NoError(t, err, "%v at %s columns: %s", args, columns, errs)
	return out
}

// SameAtAnyWidth runs a command that prints only a document, a usage
// page, a completion script, a Markdown body, at 20 columns and at
// 100,000, and holds its standard output the same at both: nothing of
// it is prose to wrap (#94).
func SameAtAnyWidth(t *testing.T, args ...string) {
	t.Helper()
	narrow := atWidth(t, "20", args...)
	wide := atWidth(t, "100000", args...)
	require.Equal(t, wide, narrow, "%v wrapped at 20 columns", args)
}

// PayloadAtAnyWidth runs a command that mixes dockhand's sentences with a
// payload, a Portfile or a diff, at 20 columns and at 100,000: the
// payload's bytes are whole in its output at both, and a sentence of
// dockhand's own wraps at 20 (#94). Whole-output equality can't hold:
// the sentences wrap.
func PayloadAtAnyWidth(t *testing.T, payload string, args ...string) {
	t.Helper()
	narrow := atWidth(t, "20", args...)
	wide := atWidth(t, "100000", args...)
	require.Contains(t, narrow, payload, "%v at 20 columns", args)
	require.Contains(t, wide, payload, "%v at 100,000 columns", args)
	wrapped := false
	for line := range strings.SplitSeq(narrow, "\n") {
		if strings.HasPrefix(line, "  ") && !strings.Contains(payload, line) {
			wrapped = true
		}
	}
	require.True(t, wrapped, "%v: no sentence wrapped at 20 columns:\n%s", args, narrow)
}
