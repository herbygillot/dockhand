package record

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// started is the instant every golden in the tree carries.
var started = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// populated exercises every part of the wire format at once: a schema
// the encoder must overwrite, run keys given out of lexical order, a
// run with nothing but a state (so the always-emitted zero job shows),
// a run with a detail and no job, and a run with every omitempty field
// set and a detail carrying the three bytes encoding/json escapes.
func populated() Record {
	return Record{
		Schema: 99,
		Sha:    "7159f6b651e49cae47422560120e93ebc494acc9",
		Tree:   "84638b5a25febc78bd8ac7cad517ef4d88764262",
		Port:   "jq",
		Runs: map[string]Run{
			"Testos": {
				State:  Failed,
				Job:    verify.Job{Provider: "fake", ID: "fake-1", Started: started},
				Handle: "dockhand-worker-failed",
				Detail: `Failed to build jq: "a & b" <not> ok`,
				Tested: true,
				Linted: true,
				Lint:   "2 warnings",
			},
			"Oldos":     {State: Deferred, Detail: "all 2 verification slots are busy"},
			"Ancientos": {State: Unsupported},
		},
	}
}

// wirePopulated is what lifecycle.WriteNote marshalled for the same
// content at HEAD 29f3834, captured by encoding a lifecycle.Note built
// field for field beside populated() and comparing the bytes there.
// That type and that function are gone as of this step, so the proof
// is pinned here as bytes rather than left as an import that could not
// survive the rewire.
//
// Everything this string demonstrates is load-bearing: two-space
// indent, no trailing newline, the five top-level fields in
// declaration order, run keys sorted by encoding/json (Ancientos
// before Oldos before Testos, not the order they were written in),
// "job" emitted even when zero, the omitempty fields appearing only
// where set and in declaration order, HTML escaping left on so the
// ampersand and angle brackets in a detail come out as the \u0026,
// \u003c and \u003e escapes the notes on disk already carry, and the
// schema stamped 2 over the 99 the caller supplied.
const wirePopulated = `{
  "schema": 2,
  "sha": "7159f6b651e49cae47422560120e93ebc494acc9",
  "tree": "84638b5a25febc78bd8ac7cad517ef4d88764262",
  "port": "jq",
  "runs": {
    "Ancientos": {
      "state": "unsupported",
      "job": {
        "provider": "",
        "id": "",
        "started": "0001-01-01T00:00:00Z"
      }
    },
    "Oldos": {
      "state": "deferred",
      "job": {
        "provider": "",
        "id": "",
        "started": "0001-01-01T00:00:00Z"
      },
      "detail": "all 2 verification slots are busy"
    },
    "Testos": {
      "state": "failed",
      "job": {
        "provider": "fake",
        "id": "fake-1",
        "started": "2026-09-01T00:00:00Z"
      },
      "handle": "dockhand-worker-failed",
      "detail": "Failed to build jq: \"a \u0026 b\" \u003cnot\u003e ok",
      "tested": true,
      "linted": true,
      "lint": "2 warnings"
    }
  }
}`

// wireNilRuns is the shape a record with no runs map writes. "runs"
// carries no omitempty, so a nil map is null on the wire — and Decode
// reads that back through the legacy lift, not the fast path.
const wireNilRuns = `{
  "schema": 2,
  "sha": "abc",
  "tree": "def",
  "port": "jq",
  "runs": null
}`

const wireZero = `{
  "schema": 2,
  "sha": "",
  "tree": "",
  "port": "",
  "runs": null
}`

func TestEncodeIsTodaysWireFormat(t *testing.T) {
	for _, tc := range []struct {
		name string
		rec  Record
		want string
	}{
		{"every field exercised", populated(), wirePopulated},
		{"a nil runs map", Record{Sha: "abc", Tree: "def", Port: "jq"}, wireNilRuns},
		{"the zero record", Record{}, wireZero},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Encode(tc.rec)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

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
	// surfaces must not diverge.
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

func TestDecodeReadsBackWhatEncodeWrote(t *testing.T) {
	r := populated()
	b, err := Encode(r)
	require.NoError(t, err)
	got, err := Decode(b, r.Sha)
	require.NoError(t, err)
	want := populated()
	want.Schema = Schema
	assert.Equal(t, want, got)
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
		_, err := Decode([]byte("{not json"), sha)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "note on "+sha+" does not parse")
		assert.Contains(t, err.Error(), "`git notes --ref="+NotesRef+" remove "+sha+"` clears it")
	})

	t.Run("a schema from the future is refused, not half-read", func(t *testing.T) {
		_, err := Decode([]byte(`{"schema":99,"sha":"`+sha+`","port":"jq","runs":{}}`), sha)
		require.Error(t, err)
		assert.Equal(t,
			"note on "+sha+" was written by a newer dockhand (schema 99, this build speaks 2); upgrade dockhand",
			err.Error())
	})

	t.Run("a note describing another commit is corrupt", func(t *testing.T) {
		_, err := Decode([]byte(`{"schema":2,"sha":"ffff","port":"jq","runs":{}}`), sha)
		require.Error(t, err)
		assert.Equal(t,
			"note on "+sha+" claims to describe ffff — corrupt; `git notes --ref="+NotesRef+" remove "+sha+"` clears it",
			err.Error(),
			"the message names the commit it is attached to first, then the one it claims")
	})

	t.Run("an empty sha is not a mismatch", func(t *testing.T) {
		// A note that never recorded its own sha predates the check and
		// is read, not refused.
		got, err := Decode([]byte(`{"schema":2,"port":"jq","runs":{}}`), sha)
		require.NoError(t, err)
		assert.Empty(t, got.Sha)
	})

	t.Run("a schema-1 note is not sha-checked", func(t *testing.T) {
		// The check is conditioned on schema 2, so an older note reaches
		// the lift rather than being refused for a field it predates.
		got, err := Decode([]byte(`{"schema":1,"sha":"ffff","port":"jq","platform":"Testos","state":"passed"}`), sha)
		require.NoError(t, err)
		assert.Equal(t, "ffff", got.Sha)
	})
}

func TestDecodeLiftsASchemaOneNote(t *testing.T) {
	const sha = "7159f6b651e49cae47422560120e93ebc494acc9"
	body := `{"schema":1,"sha":"` + sha + `","tree":"84638b5","port":"jq","platform":"Testos",` +
		`"state":"failed","job":{"provider":"fake","id":"fake-1","started":"2026-09-01T00:00:00Z"},` +
		`"handle":"dockhand-worker-1","detail":"Failed to build jq"}`
	got, err := Decode([]byte(body), sha)
	require.NoError(t, err)
	assert.Equal(t, Record{
		Schema: Schema, Sha: sha, Tree: "84638b5", Port: "jq",
		Runs: map[string]Run{"Testos": {
			State:  Failed,
			Job:    verify.Job{Provider: "fake", ID: "fake-1", Started: started},
			Handle: "dockhand-worker-1",
			Detail: "Failed to build jq",
		}},
	}, got, "the flat verdict becomes one run keyed by its platform")
}

func TestDecodeLiftsAnUnrecordedPlatform(t *testing.T) {
	got, err := Decode([]byte(`{"schema":1,"port":"jq","state":"passed"}`), "abc")
	require.NoError(t, err)
	assert.Equal(t, map[string]Run{unrecordedPlatform: {State: Passed}}, got.Runs)
}

func TestDecodeLiftsASchemaTwoNoteWithNullRuns(t *testing.T) {
	// The fast path tests Runs, not the schema alone, and Encode writes
	// null for a nil map. A note in that shape has always come back as
	// one unrecorded run in the empty state, and it still does — the
	// codec does not gain a refusal here.
	b, err := Encode(Record{Sha: "abc", Tree: "def", Port: "jq"})
	require.NoError(t, err)
	got, err := Decode(b, "abc")
	require.NoError(t, err)
	assert.Equal(t, Record{
		Schema: Schema, Sha: "abc", Tree: "def", Port: "jq",
		Runs: map[string]Run{unrecordedPlatform: {}},
	}, got)
}

func TestDecodeCopiesAnUnknownStateThrough(t *testing.T) {
	// The reader has never judged a state word, and ParseRunState's
	// strictness must not leak into the codec: a note carrying a state
	// this build does not know is read, not refused, because refusing
	// would strand a record that governs worker release.
	got, err := Decode([]byte(`{"schema":2,"sha":"abc","runs":{"Testos":{"state":"quantum"}}}`), "abc")
	require.NoError(t, err)
	assert.Equal(t, RunState("quantum"), got.Runs["Testos"].State)
}
