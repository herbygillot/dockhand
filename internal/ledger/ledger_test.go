package ledger

// The ledger tests: custody of the notes, driven against real git.
// What is proven here is the boundary — that absence and refusal stay
// different answers, that the bytes on disk are the codec's plus the
// newline git adds, and that a scan hands back what git listed in the
// order git listed it.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/tool"
	"github.com/herbygillot/dockhand/internal/verify"
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

// passedOn is a settled run, the commonest thing a note holds: one
// subject, one guest on the platform, one verdict keyed to both.
func passedOn(sha string, plat string) record.Record {
	return record.Record{
		Schema: record.Schema, Sha: sha,
		Subjects: []record.Subject{{Port: "jq", Names: []string{"jq"}}},
		Jobs: map[string]record.JobRecord{plat: {
			Job:  verify.Job{Provider: "fake", ID: "fake-1", Started: started},
			Test: true,
		}},
		Runs: map[string]record.Run{record.RunKey("jq", plat): {
			State: record.Passed, Platform: plat, Linted: true, Lint: "clean",
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
	want := passedOn(sha, "Testos")

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
	r := passedOn(sha, "Testos")
	require.NoError(t, l.Write(ctx, r))

	encoded, err := record.Encode(r)
	require.NoError(t, err)
	stored, err := repo.NoteRead(ctx, git.VerifyNotesRef, sha)
	require.NoError(t, err)
	assert.Equal(t, string(encoded)+"\n", string(stored))
}

func TestWriteStampsTheSchemaWhateverTheCallerHeld(t *testing.T) {
	// A record read back from a note keeps the schema it decoded under,
	// and passes through several hands before it is written again. The
	// stamp is the codec's, and this is the proof it survives storage.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	r := passedOn(sha, "Testos")
	r.Schema = 0

	require.NoError(t, l.Write(ctx, r))
	got, err := l.Read(ctx, sha)
	require.NoError(t, err)
	assert.Equal(t, record.Schema, got.Schema)
}

func TestReadRefusesWhatItCannotHonourAndNeverAsAbsence(t *testing.T) {
	// The distinction the whole layer rests on: a note that will not
	// parse is an error, and specifically NOT git.ErrNoNote, because
	// every caller that starts fresh on absence would otherwise start
	// fresh over state that governs worker release and promotion.
	l, repo, sha := ledgerRepo(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		body string
		says string
	}{
		{"malformed", "{not json", "does not parse"},
		{"a schema from the future",
			`{"schema":99,"sha":"` + sha + `","runs":{}}`, "newer dockhand"},
		{"a schema this build no longer reads",
			`{"schema":2,"sha":"` + sha + `","port":"jq","runs":{}}`,
			"cannot be carried over"},
		{"a note describing another commit",
			`{"schema":3,"sha":"0000000000000000000000000000000000000000","runs":{}}`,
			"claims to describe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gittest.Note(t, repo, sha, tc.body)
			_, err := l.Read(ctx, sha)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.says)
			assert.NotErrorIs(t, err, git.ErrNoNote, "a refusal must never read as absence")
		})
	}
}

func TestRemoveIsIdempotent(t *testing.T) {
	// Discard sweeps every commit a branch owns, and most of them never
	// carried a note.
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.Write(ctx, passedOn(sha, "Testos")))

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
	require.NoError(t, l.Write(ctx, passedOn(sha, "Testos")))
	require.NoError(t, l.Write(ctx, passedOn(other, "Testos")))
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

// errClosure is the caller's own failure, to prove Update returns it
// rather than swallowing or renaming it.
var errClosure = errors.New("the caller's own refusal")
