package cmdtest

import (
	"encoding/json"
	"testing"

	"github.com/herbygillot/dockhand/internal/term"
	"github.com/stretchr/testify/require"
)

// Decoded is a --json envelope, read back loosely.
type Decoded struct {
	Version  int            `json:"version"`
	Command  string         `json:"command"`
	ExitCode int            `json:"exit_code"`
	Error    *string        `json:"error"`
	Result   map[string]any `json:"result"`
	// GitHubRequests is a pointer, so an envelope without it is told
	// from one that sent none.
	GitHubRequests *int64 `json:"github_requests"`
}

// JsonOf runs a command line with --json and decodes all of standard
// output as its one envelope.
func JSONOf(t *testing.T, args ...string) (Decoded, error) {
	t.Helper()
	out, _, err := Dockhand(t, append(args, "--json")...)
	var envelope Decoded
	require.NoError(t, json.Unmarshal([]byte(out), &envelope), "standard output is only the envelope: %s", out)
	require.Equal(t, 1, envelope.Version)
	if err != nil {
		require.Equal(t, term.ExitCode(err), envelope.ExitCode)
		require.Empty(t, err.Error(), "the envelope carries the error, not standard error")
	}
	return envelope, err
}

// Dig walks a decoded result by keys and indexes.
func Dig(t *testing.T, value any, path ...any) any {
	t.Helper()
	for _, step := range path {
		switch key := step.(type) {
		case string:
			object, ok := value.(map[string]any)
			require.True(t, ok, "%v is not an object at %q", value, key)
			value = object[key]
		case int:
			list, ok := value.([]any)
			require.True(t, ok && key < len(list), "%v has no item %d", value, key)
			value = list[key]
		}
	}
	return value
}
