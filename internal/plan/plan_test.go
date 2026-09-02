package plan

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/text"
)

// Encode is --plan's debugging rendering; nothing reads a plan back
// (D21). The test pins the shape a reader sees, not a wire contract.
func TestEncodeRendersThePlan(t *testing.T) {
	p := &Plan{
		Format:         Format,
		Intent:         "bump",
		Portdir:        "/tree/sysutils/foo",
		PortfileSHA256: "abc",
		Edits: []edit.Edit{
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
	out := buf.String()
	assert.Contains(t, out, `"intent": "bump"`)
	assert.Contains(t, out, `"portdir": "/tree/sysutils/foo"`)
	assert.Contains(t, out, `"old": "1.0.0"`)
	assert.Contains(t, out, `"new": "2.0.0"`)
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

// Materialize is the one precondition-then-apply; the three
// realizations that call it each wrap its errors in their own words,
// so what it returns must be bare enough for all of them: ErrDrift
// itself on a hash miss, edit.Apply's error untouched after a match.
func TestMaterialize(t *testing.T) {
	src := []byte("name jq\nversion 1.8.1\nrevision 0\n")
	good := []edit.Edit{
		{Start: 16, End: 21, Old: "1.8.1", New: "1.8.2", Reason: "version"},
	}
	// Two edits on one span: refused by text.Apply after every Old has
	// been checked, so the failure is the apply's, not a stale plan's.
	overlapping := []edit.Edit{
		{Start: 16, End: 21, Old: "1.8.1", New: "1.8.2", Reason: "version"},
		{Start: 16, End: 21, Old: "1.8.1", New: "1.9.0", Reason: "version"},
	}

	tests := []struct {
		name  string
		hash  string
		edits []edit.Edit
		check func(t *testing.T, got []byte, err error)
	}{
		{
			name:  "hash match yields edit.Apply's bytes",
			hash:  edit.FileSHA256(src),
			edits: good,
			check: func(t *testing.T, got []byte, err error) {
				require.NoError(t, err)
				want, aerr := edit.Apply(src, good)
				require.NoError(t, aerr)
				assert.Equal(t, want, got)
				assert.Equal(t, "name jq\nversion 1.8.2\nrevision 0\n", string(got))
			},
		},
		{
			name:  "hash miss is ErrDrift bare",
			hash:  edit.FileSHA256([]byte("name jq\nversion 1.8.0\nrevision 0\n")),
			edits: good,
			check: func(t *testing.T, got []byte, err error) {
				require.ErrorIs(t, err, ErrDrift)
				// Bare: the sentinel itself, with nothing wrapped around
				// it that the callers would have to speak around.
				assert.Same(t, ErrDrift, err)
				assert.Nil(t, got)
			},
		},
		{
			name:  "apply failure after a match is edit.Apply's error, not drift",
			hash:  edit.FileSHA256(src),
			edits: overlapping,
			check: func(t *testing.T, got []byte, err error) {
				require.Error(t, err)
				require.NotErrorIs(t, err, ErrDrift)
				var ee text.EditError
				require.ErrorAs(t, err, &ee)
				assert.Equal(t, text.Overlap, ee.Type)
				assert.Nil(t, got)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &Plan{PortfileSHA256: tc.hash, Edits: tc.edits}
			got, err := p.Materialize(src)
			tc.check(t, got, err)
		})
	}
}
