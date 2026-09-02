package ledger

// Custody of the audit ref, driven against real git. What is proven
// here is the property the second ref exists for: rows accumulate, a
// close is a further row rather than an edit, and a reconciler that
// runs every few minutes over a settled change writes nothing more.

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
)

// gitRun drives git in the repository directly. It is here rather than
// behind a wrapper because what it runs is housekeeping dockhand never
// performs — the collector — and the only reason to perform it is to
// prove the rows outlive it.
func gitRun(t *testing.T, repo *git.Repo, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo.Root
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

// publication is the opening row a promote would write.
func publication(sha string) record.OutcomeRow {
	return record.OutcomeRow{
		MintSha: sha, Branch: "dockhand/jq-1.8", Port: "jq", Target: "1.8",
		MintedVia: record.MintedSingle, AskedBy: record.Human,
		PublishedBy: record.Human, Evidence: record.Verified, PRNumber: 42,
		PublishedAt: "2026-09-01T00:00:00Z",
	}
}

func TestOutcomeAppendsAndSettleClosesWithASecondRow(t *testing.T) {
	l, repo, sha := ledgerRepo(t)
	ctx := context.Background()

	require.NoError(t, l.Outcome(ctx, publication(sha)))
	require.NoError(t, l.Settle(ctx, sha, record.Merged, "cafe1234", "2026-09-09T00:00:00Z"))

	rows, err := l.Outcomes(ctx, sha)
	require.NoError(t, err)
	require.Len(t, rows, 2, "the close is appended; the publication it closes is still there to read")
	assert.True(t, rows[0].Open())
	assert.Equal(t, record.Merged, rows[1].Outcome)
	assert.Equal(t, "cafe1234", rows[1].MergeSha)
	// The closing row carries the publication forward, which is what
	// makes the last line answer on its own.
	assert.Equal(t, record.Verified, rows[1].Evidence)
	assert.Equal(t, "2026-09-01T00:00:00Z", rows[1].PublishedAt)
	assert.Equal(t, 42, rows[1].PRNumber)

	// And it is genuinely on the second ref, not the verification one.
	body, err := repo.NoteRead(ctx, git.OutcomeNotesRef, sha)
	require.NoError(t, err)
	assert.Len(t, strings.Split(strings.TrimSuffix(string(body), "\n"), "\n"), 3,
		"two rows with git's blank line between them")
	_, err = repo.NoteRead(ctx, git.VerifyNotesRef, sha)
	assert.ErrorIs(t, err, git.ErrNoNote, "the audit ref is its own; nothing was written to the records")
}

func TestSettleTwiceWritesOnce(t *testing.T) {
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.Outcome(ctx, publication(sha)))
	require.NoError(t, l.Settle(ctx, sha, record.Rejected, "", "2026-09-09T00:00:00Z"))

	// A rejected branch is never retired, so every later pass reaches
	// the same verdict about it. Appending a row each time would be an
	// audit that grows without learning anything.
	require.NoError(t, l.Settle(ctx, sha, record.Rejected, "", "2026-09-10T00:00:00Z"))
	require.NoError(t, l.Settle(ctx, sha, record.Rejected, "", "2026-09-11T00:00:00Z"))

	rows, err := l.Outcomes(ctx, sha)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "2026-09-09T00:00:00Z", rows[1].SettledAt, "the first close is the one that stands")
}

func TestSettleWithNothingPublishedWritesNothing(t *testing.T) {
	l, repo, sha := ledgerRepo(t)
	ctx := context.Background()

	require.NoError(t, l.Settle(ctx, sha, record.Merged, "cafe1234", "2026-09-09T00:00:00Z"))

	_, err := repo.NoteRead(ctx, git.OutcomeNotesRef, sha)
	assert.ErrorIs(t, err, git.ErrNoNote,
		"a change this dockhand never published gets no outcome invented for it")
}

func TestOutcomesOfAnUnpublishedCommitIsNoRowsAndNoError(t *testing.T) {
	l, _, sha := ledgerRepo(t)

	rows, err := l.Outcomes(context.Background(), sha)

	require.NoError(t, err, "absence here is not the refusal it is on the verification ref")
	assert.Empty(t, rows)
}

func TestPublishingTheSameTipTwiceIsTwoPublications(t *testing.T) {
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()

	require.NoError(t, l.Outcome(ctx, publication(sha)))
	second := publication(sha)
	second.PublishedAt = "2026-09-03T00:00:00Z"
	require.NoError(t, l.Outcome(ctx, second))

	rows, err := l.Outcomes(ctx, sha)
	require.NoError(t, err)
	require.Len(t, rows, 2, "review was asked for twice, and a log that collapsed that would lose it")
	assert.True(t, rows[1].Open(), "the second publication is open and settles on its own")
}

func TestARepublishAfterACloseReopensTheSet(t *testing.T) {
	l, _, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.Outcome(ctx, publication(sha)))
	require.NoError(t, l.Settle(ctx, sha, record.Rejected, "", "2026-09-09T00:00:00Z"))

	again := publication(sha)
	again.PublishedAt = "2026-09-10T00:00:00Z"
	require.NoError(t, l.Outcome(ctx, again))
	require.NoError(t, l.Settle(ctx, sha, record.Merged, "cafe1234", "2026-09-11T00:00:00Z"))

	rows, err := l.Outcomes(ctx, sha)
	require.NoError(t, err)
	require.Len(t, rows, 4)
	assert.Equal(t, record.Rejected, rows[1].Outcome)
	assert.Equal(t, record.Merged, rows[3].Outcome,
		"re-promoting a rejected change is a new publication and gets a close of its own")
}

func TestOutcomeRowsSurviveTheCollector(t *testing.T) {
	l, repo, sha := ledgerRepo(t)
	ctx := context.Background()
	require.NoError(t, l.Outcome(ctx, publication(sha)))
	require.NoError(t, l.Settle(ctx, sha, record.Merged, "cafe1234", "2026-09-09T00:00:00Z"))

	// The ruling that put the audit in a notes ref rests on this: rows
	// outlive the branch, and the branch is the only thing keeping the
	// annotated commit reachable. Held here rather than trusted from the
	// spike that established it, because it is a property of git's
	// housekeeping and git is not ours.
	require.NoError(t, repo.DeleteBranch(ctx, "dockhand/jq-1.8"))
	gitRun(t, repo, "reflog", "expire", "--expire=now", "--all")
	gitRun(t, repo, "gc", "--prune=now")

	rows, err := l.Outcomes(ctx, sha)
	require.NoError(t, err, "only `git notes prune` removes these, and nothing here runs it")
	require.Len(t, rows, 2)
	assert.Equal(t, record.Merged, rows[1].Outcome)
}
