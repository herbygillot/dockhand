package ledger

// The ledger tests: custody of the notes, driven against real git.
// What is proven here is the boundary — that absence and refusal stay
// different answers and that the refusal arrives as an identity rather
// than as a sentence, that the bytes on disk are the codec's plus the
// newline git adds, and that a scan hands back what git listed in the
// order git listed it.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tool"
)

// realTools is the finder every fixture here carries: the real PATH
// search, because git is genuinely driven.
var realTools = tool.NewFinder(nil)

// started is the instant the goldens carry, so a note written here has
// the same shape as one written anywhere else in the tree.
var started = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// ledgerRepo is a ports-tree-shaped repository with one dockhand
// branch minted, its ledger and tip returned alongside.
func ledgerRepo(t *testing.T) (*Ledger, *git.Repo, string) {
	t.Helper()
	ctx := context.Background()
	repo := gittest.PortsTree(t, realTools)
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	sha := gittest.Commit(t, repo, "dockhand/jq-1.8", primary, "sysutils/jq/Portfile",
		"version 1.8\n", "jq: update to 1.8")
	return Open(repo), repo, sha
}

// exported is the commonest projection a note holds: one change with
// one subject, the guest it was built in, and the verdict keyed to the
// pair. It is what statestore.Export hands Write, written out here by
// hand because the store arrives a step later.
func exported(sha, plat string) record.Record {
	return record.Record{
		Schema: record.Schema, Sha: sha,
		Change: record.Change{
			Schema:   record.DocSchema,
			ID:       "chg-01HZ",
			State:    record.ChangeMinted,
			Branch:   "dockhand/jq-1.8",
			Tip:      sha,
			Slug:     "jq-1.8",
			Content:  "sha256:9f2c",
			Subjects: []record.Subject{{Port: "jq", Names: []string{"jq"}}},
		},
		Runs: map[record.RunKey]record.Run{
			{Port: "jq", Platform: plat}: {
				State: record.Passed, Content: "sha256:9f2c", At: started,
			},
		},
		Leases: map[string]record.Lease{plat: {
			Schema:   record.DocSchema,
			ID:       record.LeaseID{Provider: "fake", ID: "fake-1", Started: started},
			Request:  "req-01HZ",
			Change:   "chg-01HZ",
			Platform: plat,
			Phase:    record.Finished,
		}},
	}
}

func TestReadAnswersAbsenceForAnUnnotedCommit(t *testing.T) {
	l, _, sha := ledgerRepo(t)
	_, err := l.Read(context.Background(), sha)
	assert.ErrorIs(t, err, git.ErrNoNote)
}

func TestWriteThenReadRoundTripsTheRecord(t *testing.T) {
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	want := exported(sha, "Testos")

	require.NoError(t, l.Write(ctx, want))
	got, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestTheCodecNamesTheRefTheLedgerWritesTo(t *testing.T) {
	// record is a leaf and cannot import git, so it restates the ref by
	// hand for the one purpose it needs it: the refusals it writes tell
	// a user which namespace to clear. Nothing else compares the two
	// spellings — the exit table builds its own copy of that sentence
	// for band mapping and never reads the codec's — so a rename of
	// git.VerifyNotesRef would leave record pointing users at a ref that
	// no longer exists, and every test would stay green. This package
	// imports both, which makes it the only place the drift can be
	// caught.
	assert.Equal(t, git.VerifyNotesRef, record.NotesRef)
}

func TestWriteStoresTheCodecsBytesAndGitsNewline(t *testing.T) {
	// The byte contract of the store, stated where the two halves meet:
	// the note holds exactly what Encode produced, plus the final
	// newline `git notes add` completes the last line with. A reader
	// comparing a stored note to Encode's output alone is off by one
	// byte, and a writer that appended its own newline would be off by
	// one the other way.
	l, repo, sha := ledgerRepo(t)
	ctx := context.Background()
	r := exported(sha, "Testos")
	require.NoError(t, l.Write(ctx, r))

	encoded, err := record.Encode(r)
	require.NoError(t, err)
	stored, err := repo.NoteRead(ctx, git.VerifyNotesRef, sha)
	require.NoError(t, err)
	assert.Equal(t, string(encoded)+"\n", string(stored))
}

func TestWriteStampsTheSchemaWhateverTheCallerHeld(t *testing.T) {
	// A record decoded under one schema and handed straight back must
	// not be written out claiming to be what it was. The stamp is the
	// codec's, and this is the proof it survives storage.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	r := exported(sha, "Testos")
	r.Schema = 0

	require.NoError(t, l.Write(ctx, r))
	got, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Schema, got.Schema)
}

func TestReadRefusesWhatItCannotHonourAndNeverAsAbsence(t *testing.T) {
	// The distinction the layer rests on: a note that will not parse is
	// an error, and specifically NOT git.ErrNoNote, because a commit
	// the export has not reached and a commit whose export is corrupt
	// are different facts and a reader that could not tell them apart
	// would report a broken notes ref as an empty one.
	//
	// Each row asserts the IDENTITY and not the sentence. The message a
	// person reads is built at the point of refusal and may be reworded
	// without moving what code matches; that is the whole reason the
	// codec declares sentinels.
	l, repo, sha := ledgerRepo(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		body string
		is   error
	}{
		{"malformed", "{not json", record.ErrMalformed},
		{"a schema from the future",
			`{"schema":99,"sha":"` + sha + `"}`, record.ErrSchemaTooNew},
		{"every note this bump refuses",
			`{"schema":3,"sha":"` + sha + `","port":"jq","runs":{}}`,
			record.ErrSchemaTooOld},
		{"a note describing another commit",
			`{"schema":4,"sha":"0000000000000000000000000000000000000000"}`,
			record.ErrShaMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gittest.Note(t, repo, sha, tc.body)
			_, err := l.Read(ctx, sha)
			require.Error(t, err)
			require.ErrorIs(t, err, tc.is)
			assert.NotErrorIs(t, err, git.ErrNoNote, "a refusal must never read as absence")
		})
	}
}

func TestTheSchemaRefusalNamesTheRemedyThisBuildCanHonour(t *testing.T) {
	// Schema 4 refuses every note in every checkout, which is ruled and
	// cheap: the note is a projection and the store still holds what
	// happened. The sentence has to be true of THIS build, though — the
	// ref it tells a person to clear is the ref this package writes to,
	// and the commit it names is the one they asked about — or the
	// remedy sends them somewhere the stale note is not.
	l, repo, sha := ledgerRepo(t)
	gittest.Note(t, repo, sha, `{"schema":3,"sha":"`+sha+`","port":"jq","runs":{}}`)

	_, err := l.Read(context.Background(), sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git notes --ref="+git.VerifyNotesRef+" remove "+sha)
}

func TestRemoveIsIdempotent(t *testing.T) {
	// Discard sweeps every commit a branch owns, and most of them never
	// carried a note.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.Write(ctx, exported(sha, "Testos")))

	require.NoError(t, l.Remove(ctx, sha))
	require.NoError(t, l.Remove(ctx, sha), "removing an unnoted commit is not an error")
	_, err := l.Read(ctx, sha)
	assert.ErrorIs(t, err, git.ErrNoNote)
}

func TestAllListsEveryAnnotatedCommit(t *testing.T) {
	l, repo, sha := ledgerRepo(t)
	ctx := context.Background()
	primary, err := repo.PrimaryBranch(ctx)
	require.NoError(t, err)
	other := gittest.Commit(t, repo, "dockhand/jq-1.9", primary, "sysutils/jq/Portfile",
		"version 1.9\n", "jq: update to 1.9")

	assert.Empty(t, mustAll(t, l), "a repository with no notes annotates nothing")
	require.NoError(t, l.Write(ctx, exported(sha, "Testos")))
	require.NoError(t, l.Write(ctx, exported(other, "Testos")))
	assert.ElementsMatch(t, []string{sha, other}, mustAll(t, l))

	require.NoError(t, l.Remove(ctx, other))
	assert.Equal(t, []string{sha}, mustAll(t, l), "a removed note leaves the listing")
}

func mustAll(t *testing.T, l *Ledger) []string {
	t.Helper()
	shas, err := l.All(context.Background())
	require.NoError(t, err)
	return shas
}
