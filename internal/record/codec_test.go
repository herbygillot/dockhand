package record

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeLeavesTheCallersRecordAlone(t *testing.T) {
	// The schema is stamped on Encode's own copy, so a record read back
	// under one number and written under another does not mutate under
	// the caller's hand.
	r := Record{Schema: 99, Sha: "abc"}
	b, err := Encode(r)
	require.NoError(t, err)
	assert.Equal(t, 99, r.Schema, "the caller's value is untouched")
	assert.Contains(t, string(b), `"schema": 4`)
}

func TestDecodeReadsANoteAsGitStoredIt(t *testing.T) {
	// git completes the final line when it stores a note, so what comes
	// back is Encode's bytes plus one newline.
	b, err := Encode(populated())
	require.NoError(t, err)
	got, err := Decode(append(b, '\n'), populated().Sha)
	require.NoError(t, err)
	assert.Equal(t, ChangeID("chg-01HZ"), got.Change.ID)
}

func TestDecodeRefusesWhatItCannotHonour(t *testing.T) {
	// Four refusals, matched as identities. The sentence a person reads
	// is built at the point of refusal and is not what code branches on.
	tests := []struct {
		name    string
		body    string
		wantSha string
		want    error
		// remedy says the sentence tells a person which ref to clear.
		// Three of the four do; the note a NEWER dockhand wrote does not,
		// because clearing it would throw away a record this build simply
		// cannot read and the remedy is to upgrade instead.
		remedy bool
	}{
		{
			name:    "bytes that are not a record at all",
			body:    "{not json",
			wantSha: "abc",
			want:    ErrMalformed,
			remedy:  true,
		},
		{
			name:    "a note a newer dockhand wrote",
			body:    `{"schema":99,"sha":"abc","tree":"def"}`,
			wantSha: "abc",
			want:    ErrSchemaTooNew,
		},
		{
			name:    "a note from before this shape",
			body:    `{"schema":3,"sha":"abc","tree":"def"}`,
			wantSha: "abc",
			want:    ErrSchemaTooOld,
			remedy:  true,
		},
		{
			name:    "a note describing another commit",
			body:    `{"schema":4,"sha":"abc","tree":"def"}`,
			wantSha: "999",
			want:    ErrShaMismatch,
			remedy:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.body), tt.wantSha)
			require.Error(t, err)
			require.ErrorIs(t, err, tt.want)
			if tt.remedy {
				assert.Contains(t, err.Error(), NotesRef,
					"the sentence names the ref a person would clear")
			} else {
				assert.Contains(t, err.Error(), "upgrade dockhand")
			}
		})
	}
}

func TestAnOlderSchemaIsRefusedBeforeTheShaIsLookedAt(t *testing.T) {
	// A note from another schema is unreadable whatever commit it names,
	// and checking the sha first would answer it with a sentence about
	// corruption.
	_, err := Decode([]byte(`{"schema":3,"sha":"someone-else","tree":"def"}`), "abc")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrSchemaTooOld)
	require.NotErrorIs(t, err, ErrShaMismatch)
}

func TestMalformedWrapsTheJSONErrorUnderneath(t *testing.T) {
	// Both the identity and the cause stay reachable, which is the whole
	// reason refusal unwraps to two errors.
	_, err := Decode([]byte("{not json"), "abc")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMalformed)
	var syn *json.SyntaxError
	require.ErrorAs(t, err, &syn)
}

func TestANoteWithNoShaOfItsOwnIsNotACopy(t *testing.T) {
	// The mismatch check is for the note that names the WRONG commit. A
	// note that names none cannot be a copy of another one's.
	got, err := Decode([]byte(`{"schema":4,"tree":"def"}`), "abc")
	require.NoError(t, err)
	assert.Empty(t, got.Sha)
}

func TestARunKeyIsAPairOnTheWireAndAStructInMemory(t *testing.T) {
	// encoding/json will not use a struct as a map key at all, so the
	// join happens here and in UnmarshalText and in no third place.
	k := RunKey{Port: "py312-foo", Platform: "Ancientos"}
	b, err := k.MarshalText()
	require.NoError(t, err)
	assert.Equal(t, "py312-foo@Ancientos", string(b))

	var back RunKey
	require.NoError(t, back.UnmarshalText(b))
	assert.Equal(t, k, back)
}

func TestARunKeyWithNoPlatformIsRefused(t *testing.T) {
	// Answering with a half-filled pair would put every unparseable key
	// in one bucket keyed by the empty platform — a collision the reader
	// could never see. The three hand-written scanners this replaces
	// each answered "" and carried on.
	var k RunKey
	err := k.UnmarshalText([]byte("jq"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMalformedRunKey)
}

func TestARunKeySplitsOnTheFirstSeparator(t *testing.T) {
	// Neither half can carry an "@", so a key with a second one is
	// malformed rather than ambiguous — and the platform keeps it, which
	// makes the corruption visible instead of silently discarding bytes.
	var k RunKey
	require.NoError(t, k.UnmarshalText([]byte("jq@Testos@extra")))
	assert.Equal(t, RunKey{Port: "jq", Platform: "Testos@extra"}, k)
}

func TestAMalformedRunKeyInANoteIsARefusalAndNotAnEmptyMap(t *testing.T) {
	// The refusal reaches Decode, which reports it as a note that does
	// not parse: a run map read with a key nobody could place is exactly
	// the silent zero-fill DocSchema's comment is about.
	_, err := Decode([]byte(`{"schema":4,"sha":"abc","tree":"def","runs":{"jq":{"state":"passed"}}}`), "abc")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMalformed)
	require.ErrorIs(t, err, ErrMalformedRunKey)
}
