package record

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The schema itself — every field, in order, refused when it is not
// this one — is pinned in schema_test.go. What is here is the codec's
// own behaviour around those bytes.

func TestEncodeLeavesTheCallersRecordAlone(t *testing.T) {
	// The schema is stamped on Encode's own copy. Callers pass a record
	// they read back from a note and keep using it afterwards.
	r := populated()
	_, err := Encode(r)
	require.NoError(t, err)
	assert.Equal(t, 99, r.Schema, "the caller's schema is not rewritten under it")
}

func TestEncodeAgreesWithTheStatusJSONEncoder(t *testing.T) {
	// status --json re-marshals the record through json.Encoder with
	// the same indent and HTML escaping left on, so the two wire
	// surfaces must not diverge. The record is the public surface of
	// that verb, which is why it is checked rather than assumed.
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	r := populated()
	r.Schema = Schema
	require.NoError(t, enc.Encode(r))
	got, err := Encode(populated())
	require.NoError(t, err)
	// Encoder appends a newline; MarshalIndent does not.
	assert.Equal(t, b.String(), string(got)+"\n")
}

func TestDecodeReadsANoteAsGitStoredIt(t *testing.T) {
	// git completes the final line when it stores a note, so what comes
	// back is always Encode's bytes plus a newline.
	r := populated()
	b, err := Encode(r)
	require.NoError(t, err)
	got, err := Decode(append(b, '\n'), r.Sha)
	require.NoError(t, err)
	want := populated()
	want.Schema = Schema
	assert.Equal(t, want, got)
}

func TestDecodeRefusesWhatItCannotHonour(t *testing.T) {
	const sha = "7159f6b651e49cae47422560120e93ebc494acc9"

	t.Run("malformed bytes are an error, never an absence", func(t *testing.T) {
		// A corrupt note that read as "no note" would silently authorize
		// a fresh start over state that governs worker release.
		_, err := Decode([]byte("{not json"), sha)
		require.ErrorIs(t, err, ErrMalformed)
		assert.Contains(t, err.Error(), "note on "+sha+" does not parse")
		assert.Contains(t, err.Error(), "`git notes --ref="+NotesRef+" remove "+sha+"` clears it")
	})

	t.Run("the parse error itself stays reachable", func(t *testing.T) {
		// The refusal carries both its identity and its cause, so a
		// caller can match the first and a reader still gets the second.
		_, err := Decode([]byte("{not json"), sha)
		var syn *json.SyntaxError
		require.ErrorAs(t, err, &syn)
	})

	t.Run("a schema from the future is refused, not half-read", func(t *testing.T) {
		_, err := Decode([]byte(`{"schema":99,"sha":"`+sha+`","runs":{}}`), sha)
		require.ErrorIs(t, err, ErrSchemaTooNew)
		assert.Equal(t,
			"note on "+sha+" was written by a newer dockhand (schema 99, this build speaks 3); upgrade dockhand",
			err.Error())
	})

	t.Run("a note describing another commit is corrupt", func(t *testing.T) {
		_, err := Decode([]byte(`{"schema":3,"sha":"ffff","runs":{}}`), sha)
		require.ErrorIs(t, err, ErrShaMismatch)
		assert.Equal(t,
			"note on "+sha+" claims to describe ffff — corrupt; `git notes --ref="+NotesRef+" remove "+sha+"` clears it",
			err.Error(),
			"the message names the commit it is attached to first, then the one it claims")
	})

	t.Run("an empty sha is not a mismatch", func(t *testing.T) {
		// A note that records no sha names no commit and cannot be a copy
		// of another one's. The check is for the note that names the
		// wrong commit.
		got, err := Decode([]byte(`{"schema":3,"runs":{}}`), sha)
		require.NoError(t, err)
		assert.Empty(t, got.Sha)
	})
}

func TestDecodeCopiesAnUnknownStateThrough(t *testing.T) {
	// The reader does not judge a state word, and ParseRunState's
	// strictness must not leak into the codec: a note carrying a state
	// this build does not know is read, not refused, because refusing
	// would strand a record that governs worker release.
	got, err := Decode([]byte(`{"schema":3,"sha":"abc","runs":{"jq@Testos":{"state":"quantum"}}}`), "abc")
	require.NoError(t, err)
	assert.Equal(t, RunState("quantum"), got.Runs["jq@Testos"].State)
}

func TestDecodeOfANoteWithNoRuns(t *testing.T) {
	// A branch minted and never submitted. Its note is a real record
	// with no runs, not a record to be repaired: nothing is invented for
	// the empty map, and nothing refuses it.
	b, err := Encode(Record{Sha: "abc", Tree: "def", Slug: "jq-1.9", Destination: ToBranch})
	require.NoError(t, err)
	got, err := Decode(b, "abc")
	require.NoError(t, err)
	assert.Equal(t, Record{
		Schema: Schema, Sha: "abc", Tree: "def", Slug: "jq-1.9", Destination: ToBranch,
	}, got)
	assert.Nil(t, got.Runs)
	assert.Nil(t, got.Jobs)
}
