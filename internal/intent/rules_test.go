package intent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/edit"
)

// The rule sweep is a list so that a test can be about a rule that does
// not exist. Both halves of the double proof are gates on what a rule
// produced, and a gate can only be shown to work against something it
// has to stop — which means the tests need rules that misbehave, and the
// tree must not ship any.

// withRules swaps the sweep for one rule for the duration of a test, and
// puts the real one back afterwards.
func withRules(t *testing.T, name string, mk func(src []byte) (edit.Edit, bool)) {
	t.Helper()
	saved := rules
	rules = []rule{{Rule(name), mk}}
	t.Cleanup(func() { rules = saved })
}

// indexOf locates a fixture's own text, failing the test rather than
// returning -1: an edit computed from a needle that is not there would
// be a span nobody meant to write.
func indexOf(t *testing.T, haystack, needle string) int {
	t.Helper()
	at := strings.Index(haystack, needle)
	require.GreaterOrEqual(t, at, 0, "%q is not in the fixture", needle)
	return at
}

// mustApply is edit.Apply for a test that is about what the result
// contains rather than about whether the set applies.
func mustApply(t *testing.T, src []byte, edits []edit.Edit) []byte {
	t.Helper()
	out, err := edit.Apply(src, edits)
	require.NoError(t, err)
	return out
}
