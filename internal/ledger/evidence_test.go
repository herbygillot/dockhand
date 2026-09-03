package ledger

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/gittest"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// noteOn writes a record the way production does — started from the
// commit, so it carries the content identity the same-tree scan reads
// rather than a hand-built zero tree. The runs are given per platform
// and keyed here, because jq is the only subject these tests have.
func noteOn(t *testing.T, l *Ledger, sha string, runs map[string]record.Run) {
	t.Helper()
	ctx := context.Background()
	r, err := l.LoadOrStart(ctx, sha)
	require.NoError(t, err)
	r.Subjects = []record.Subject{{Port: "jq", Names: []string{"jq"}}}
	for plat, run := range runs {
		run.Platform = plat
		r.Jobs[plat] = record.JobRecord{
			Job: verify.Job{Provider: "fake", ID: "fake-" + plat, Started: started},
		}
		r.Runs[record.RunKey("jq", plat)] = run
	}
	require.NoError(t, l.Write(ctx, r))
}

// pass and fail are the two runs the gate actually weighs. Nothing
// about the environment is on either of them any more: the job and the
// handle belong to the guest, and the guest is the record's other map.
var (
	pass = record.Run{State: record.Passed, Linted: true, Lint: "clean"}
	fail = record.Run{
		State:  record.Failed,
		Detail: "Failed to build jq: command execution failed",
	}
)

// amended mints a second commit over the identical content with a
// different message: the same tree on a different sha, which is what a
// message-only amend leaves behind and what the same-tree scan exists
// to find.
func amended(t *testing.T, repo *git.Repo) string {
	t.Helper()
	primary, err := repo.PrimaryBranch(context.Background())
	require.NoError(t, err)
	return gittest.Commit(t, repo, "dockhand/jq-1.8-reworded", primary,
		"sysutils/jq/Portfile", "version 1.8\n", "jq: update to 1.8 (reworded)")
}

func TestEvidenceForReadsTheTipsOwnRecord(t *testing.T) {
	l, _, sha := ledgerRepo(t)
	noteOn(t, l, sha, map[string]record.Run{"Testos": pass})

	r, promotable, err := l.EvidenceFor(context.Background(), sha)
	require.NoError(t, err)
	assert.True(t, promotable)
	assert.Equal(t, sha, r.Sha)
}

func TestEvidenceForReportsTheTipsOwnRefusal(t *testing.T) {
	// A failure on the tip is evidence too: the record comes back, and
	// it does not clear the gate.
	l, _, sha := ledgerRepo(t)
	noteOn(t, l, sha, map[string]record.Run{"Testos": pass, "Oldos": fail})

	r, promotable, err := l.EvidenceFor(context.Background(), sha)
	require.NoError(t, err)
	assert.False(t, promotable, "a failure alongside the pass is the question review asks")
	assert.Len(t, r.Runs, 2, "the record still comes back, so the caller can say why")
}

func TestEvidenceForRefusesACorruptTipNote(t *testing.T) {
	// The refusal propagates and the scan is never reached: reading a
	// corrupt tip note as absence would let an older same-tree record
	// authorize the publication instead.
	l, repo, sha := ledgerRepo(t)
	ctx := context.Background()

	for _, tc := range []struct{ name, body, says string }{
		{"malformed", "{not json", "does not parse"},
		{"a schema from the future",
			`{"schema":99,"sha":"` + sha + `","runs":{}}`, "newer dockhand"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gittest.Note(t, repo, sha, tc.body)
			_, promotable, err := l.EvidenceFor(ctx, sha)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.says)
			assert.False(t, promotable)
		})
	}
}

func TestEvidenceForFindsARecordOverTheIdenticalTree(t *testing.T) {
	// The amend case: the verdict lives on a sha the branch no longer
	// points at, and the tree is what still matches.
	l, repo, sha := ledgerRepo(t)
	noteOn(t, l, sha, map[string]record.Run{"Testos": pass})
	reworded := amended(t, repo)
	require.NotEqual(t, sha, reworded)

	r, promotable, err := l.EvidenceFor(context.Background(), reworded)
	require.NoError(t, err)
	assert.True(t, promotable, "the content was verified, and only the message moved")
	assert.Equal(t, sha, r.Sha, "the record comes back on the sha that actually carries it")
}

func TestEvidenceForIgnoresASameTreeRecordThatDoesNotClearTheGate(t *testing.T) {
	l, repo, sha := ledgerRepo(t)
	noteOn(t, l, sha, map[string]record.Run{"Testos": fail})

	r, promotable, err := l.EvidenceFor(context.Background(), amended(t, repo))
	require.NoError(t, err)
	assert.False(t, promotable)
	assert.Empty(t, r.Sha, "nothing authorizes the tip, so nothing is handed back")
}

func TestEvidenceForOnATipWithNoEvidenceAtAll(t *testing.T) {
	l, _, sha := ledgerRepo(t)
	r, promotable, err := l.EvidenceFor(context.Background(), sha)
	require.NoError(t, err, "an unverified tip is not an error, it is unverified")
	assert.False(t, promotable)
	assert.Empty(t, r.Sha)
}

func TestEvidenceForStopsWhenTheScanMeetsANoteItCannotRead(t *testing.T) {
	// A note this build cannot read is one it cannot rule out as the
	// evidence, so the scan refuses rather than reporting "unverified"
	// over an unread record.
	l, repo, sha := ledgerRepo(t)
	gittest.Note(t, repo, sha, "{not json")

	_, promotable, err := l.EvidenceFor(context.Background(), amended(t, repo))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not parse")
	assert.False(t, promotable)
}
