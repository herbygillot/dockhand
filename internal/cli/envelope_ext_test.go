package cli_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// decodeResult unwraps a JSON envelope and decodes its result.
func decodeResult(t *testing.T, raw []byte, v any) {
	t.Helper()
	var envelope struct {
		Command  string          `json:"command"`
		ExitCode int             `json:"exit_code"`
		Error    string          `json:"error"`
		Result   json.RawMessage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &envelope), "%s", raw)
	require.NotEmpty(t, envelope.Command)
	require.NoError(t, json.Unmarshal(envelope.Result, v))
}
