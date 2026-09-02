// Package corpustest reads the settle corpus's .expect sidecars.
//
// The corpus at internal/lifecycle/testdata/logs is swept twice — the
// judgments alone in internal/verdict, and the effectful settle that
// carries them out in internal/lifecycle — over one copy of the files,
// so that a real `dockhand log` capture dropped in is picked up by both
// with no code change. What a sidecar's keys mean, which words each one
// admits, and the invariant tying a blamed port to a blocked state are
// facts about the corpus rather than about either sweep, so they are
// stated once, here.
//
// It sits outside both packages deliberately. verdict's purity is held
// at its import block, tests included, and a sidecar reader living in
// lifecycle would put the effectful sweep's own package inside it.
package corpustest

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Expect is a .expect sidecar: the two inputs a log alone cannot carry
// — the port under test, and what the guest's state file said — and the
// judgment the readers must reach from it.
type Expect struct {
	Port, Outcome, State, Blamed, Detail, Lint string
}

// Read parses the key: value sidecar. It is strict about keys and
// enumerations because a typo in a hand-written expectation must fail
// the sweep by name, never pass as an empty string.
func Read(t *testing.T, path string) Expect {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "every corpus log needs its sidecar; see the corpus README.md")
	var e Expect
	seen := map[string]bool{}
	for line := range strings.Lines(string(raw)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		require.True(t, ok, "%s: %q is not a key: value line", path, line)
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		require.False(t, seen[key], "%s: %s given twice", path, key)
		seen[key] = true
		switch key {
		case "port":
			e.Port = value
		case "outcome":
			e.Outcome = value
		case "state":
			e.State = value
		case "blamed":
			e.Blamed = value
		case "detail":
			e.Detail = value
		case "lint":
			e.Lint = value
		default:
			require.Failf(t, "unknown sidecar key", "%s: %q; the keys are port, outcome, state, blamed, detail, lint", path, key)
		}
	}
	require.NotEmpty(t, e.Port, "%s: port names what the note names", path)
	require.Contains(t, []string{"passed", "failed"}, e.Outcome, "%s: outcome is the guest's state file", path)
	require.Contains(t, []string{"passed", "failed", "blocked", "unsupported"}, e.State, "%s: state", path)
	if e.Outcome == "passed" {
		require.Equal(t, "passed", e.State, "%s: a passing guest settles as passed", path)
		require.Empty(t, e.Detail, "%s: a pass carries no detail", path)
	}
	require.Equal(t, e.State == "blocked", e.Blamed != "", "%s: blamed is set exactly when blocked", path)
	return e
}
