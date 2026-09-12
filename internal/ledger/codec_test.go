package ledger_test

import (
	"encoding/json"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/stretchr/testify/require"
)

func TestCodecRoundTripAndRejectsCorruption(t *testing.T) {
	state := ledger.NewState()
	state.Changes["change"] = record.Change{ID: "change", Disposition: record.ChangeOpen}
	state.Jobs["job"] = record.Job{ID: "job", RequestID: "request", Spec: record.JobSpec{Targets: []record.Target{{Name: "café", Variants: map[string]bool{"debug": true}}}}}
	state.Requests["request"] = "job"
	data, err := ledger.Encode(state)
	require.NoError(t, err)
	decoded, err := ledger.Decode(data)
	require.NoError(t, err)
	require.Equal(t, state, decoded, "round trip changed records: %#v", decoded)

	encodedAgain, err := ledger.Encode(decoded)
	require.NoError(t, err)
	require.Equal(t, string(data), string(encodedAgain), "unchanged state has unstable encoding")

	tests := []struct {
		name string
		edit func(map[string]any)
		want error
	}{
		{"unknown schema", func(d map[string]any) { d["schema"] = 999 }, ledger.ErrSchema},
		{"missing schema", func(d map[string]any) { delete(d, "schema") }, ledger.ErrSchema},
		{"unknown document field", func(d map[string]any) { d["future"] = true }, ledger.ErrInvalidState},
		{"missing state", func(d map[string]any) { delete(d, "state") }, ledger.ErrInvalidState},
		{"null state", func(d map[string]any) { d["state"] = nil }, ledger.ErrInvalidState},
		{"unknown state field", func(d map[string]any) { d["state"].(map[string]any)["Typo"] = true }, ledger.ErrInvalidState},
		{"unknown record field", func(d map[string]any) {
			d["state"].(map[string]any)["Changes"].(map[string]any)["change"].(map[string]any)["Typo"] = true
		}, ledger.ErrInvalidState},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var document map[string]any
			require.NoError(t, json.Unmarshal(data, &document))
			test.edit(document)
			input, err := json.Marshal(document)
			require.NoError(t, err)
			_, err = ledger.Decode(input)
			require.ErrorIs(t, err, test.want, "Decode: %v, want %v", err, test.want)
		})
	}
	for _, suffix := range []string{"{}", "null", "garbage"} {
		_, err := ledger.Decode(append(append([]byte{}, data...), suffix...))
		require.ErrorIs(t, err, ledger.ErrInvalidState, "accepted trailing %q: %v", suffix, err)
	}
	for _, input := range []string{"", "{", "null"} {
		_, err := ledger.Decode([]byte(input))
		require.Error(t, err, "accepted %q", input)
	}
}

func TestCodecRequiresCollectionsAndConsistentIdentities(t *testing.T) {
	tests := []struct {
		name string
		edit func(*ledger.State)
	}{
		{"mismatched change key", func(s *ledger.State) { s.Changes["a"] = record.Change{ID: "b"} }},
		{"empty identity", func(s *ledger.State) { s.Changes[""] = record.Change{} }},
		{"mismatched plan key", func(s *ledger.State) { s.Plans["a"] = record.VerificationPlan{JobID: "b"} }},
		{"missing request index", func(s *ledger.State) { s.Jobs["job"] = record.Job{ID: "job", RequestID: "request"} }},
		{"dangling request", func(s *ledger.State) { s.Requests["request"] = "absent" }},
		{"wrong request", func(s *ledger.State) { s.Jobs["job"] = record.Job{ID: "job", RequestID: "a"}; s.Requests["b"] = "job" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := ledger.NewState()
			test.edit(&state)
			_, err := ledger.Encode(state)
			require.ErrorIs(t, err, ledger.ErrInvalidState, "Encode: %v", err)

			raw, err := json.Marshal(map[string]any{"schema": ledger.Schema, "state": state})
			require.NoError(t, err)
			_, err = ledger.Decode(raw)
			require.ErrorIs(t, err, ledger.ErrInvalidState, "Decode: %v", err)
		})
	}
	data, err := ledger.Encode(ledger.NewState())
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, json.Unmarshal(data, &document))
	collections := document["state"].(map[string]any)
	for name, value := range collections {
		t.Run("missing "+name, func(t *testing.T) {
			delete(collections, name)
			defer func() { collections[name] = value }()
			input, err := json.Marshal(document)
			require.NoError(t, err)
			_, err = ledger.Decode(input)
			require.ErrorIs(t, err, ledger.ErrInvalidState, "Decode: %v", err)
		})
	}
}
