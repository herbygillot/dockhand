package plan

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	p := &Plan{
		Format:         Format,
		Intent:         "bump",
		Portdir:        "/tree/sysutils/foo",
		PortfileSHA256: "abc",
		Edits: []Edit{
			{Start: 10, End: 15, Old: "1.0.0", New: "2.0.0", Reason: "version"},
		},
		Predicted: []ContextDelta{
			{Subport: "foo", Changes: []Change{
				{Field: "version", Old: []string{"1.0.0"}, New: []string{"2.0.0"}},
			}},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, p.Encode(&buf))
	got, err := Decode(&buf)
	require.NoError(t, err)
	assert.Equal(t, p, got)
}

func TestDecodeRejectsWrongFormat(t *testing.T) {
	_, err := Decode(bytes.NewBufferString(`{"format": 99}`))
	require.ErrorIs(t, err, ErrFormat)
}

func TestFromDeltaCanonical(t *testing.T) {
	d := info.Delta{
		Changed: map[info.SubportKey][]info.FieldChange{
			{Subport: "zeta"}: {{Field: info.FieldVersion, Old: []string{"1"}, New: []string{"2"}}},
			{Subport: "alpha"}: {
				{Field: info.FieldVersion, Old: []string{"1"}, New: []string{"2"}},
				{Field: info.FieldChecksums, Old: []string{"x"}, New: []string{"y"}},
			},
		},
		Added: map[info.SubportKey]info.Values{
			{Subport: "new-sub"}: {Name: "new-sub", Version: "2"},
		},
	}
	wire := FromDelta(d)
	require.Len(t, wire, 3)
	// Contexts sorted by subport; changes sorted by field name.
	assert.Equal(t, "alpha", wire[0].Subport)
	assert.Equal(t, "new-sub", wire[1].Subport)
	assert.True(t, wire[1].Added)
	assert.Equal(t, "zeta", wire[2].Subport)
	assert.Equal(t, "checksums", wire[0].Changes[0].Field)
	assert.Equal(t, "version", wire[0].Changes[1].Field)
	// An added context renders one-sided: no Old values.
	for _, ch := range wire[1].Changes {
		assert.Empty(t, ch.Old, ch.Field)
		assert.NotEmpty(t, ch.New, ch.Field)
	}
}
