package engine

// The audit rows end to end: a publication opens one, a reconciliation
// closes it from what the forge said, and the demolition that removes
// the branch leaves it standing. The last of those is the reason there
// are two notes refs at all, so it is held here rather than left to the
// fact that today's code happens not to reach for the second one.

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/ledger"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/render"
	"github.com/herbygillot/dockhand/internal/verify/verifytest"
)

// published is the row a promote of the fixture branch would leave.
func published(t *testing.T, eng *Engine, repo *git.Repo, sha string) {
	t.Helper()
	require.NoError(t, eng.Publish(context.Background(), repo, Publication{
		MintSha: sha, Branch: "dockhand/jq-1.8", Port: "jq",
		PRNumber: 9, Verified: true, Invoker: record.Human,
	}))
}

func rows(t *testing.T, repo *git.Repo, sha string) []record.OutcomeRow {
	t.Helper()
	out, err := ledger.Open(repo).Outcomes(context.Background(), sha)
	require.NoError(t, err)
	return out
}

func TestPublishOpensARowAndSaysNothing(t *testing.T) {
	repo, sha := engineRepo(t)
	var out, errOut bytes.Buffer
	eng := testEngine(t, repo, &verifytest.Fake{}, &out, &errOut)

	before := time.Now()
	published(t, eng, repo, sha)

	got := rows(t, repo, sha)
	require.Len(t, got, 1)
	row := got[0]
	assert.Equal(t, sha, row.MintSha)
	assert.Equal(t, "dockhand/jq-1.8", row.Branch)
	assert.Equal(t, "jq", row.Port)
	assert.Equal(t, "1.8", row.Target, "the slug's remainder after the port is what the change moves it to")
	assert.Equal(t, record.MintedSingle, row.MintedVia)
	assert.Equal(t, record.Human, row.AskedBy)
	assert.Equal(t, record.Human, row.PublishedBy)
	assert.Equal(t, record.Verified, row.Evidence)
	assert.Equal(t, 9, row.PRNumber)
	assert.True(t, row.Open())

	at, err := time.Parse(time.RFC3339, row.PublishedAt)
	require.NoError(t, err, "the stamp is RFC 3339 in UTC")
	assert.False(t, at.Before(before.Truncate(time.Second)))

	// The verb has already said its piece. Bookkeeping the user did not
	// ask about must not add a word to either stream.
	assert.Empty(t, out.String())
	assert.Empty(t, errOut.String())
}

func TestPublishRecordsTheClaimItPublishedUnder(t *testing.T) {
	repo, sha := engineRepo(t)
	eng := testState(t, repo, &verifytest.Fake{})
	require.NoError(t, eng.Publish(context.Background(), repo, Publication{
		MintSha: sha, Branch: "dockhand/jq-1.8", Port: "jq", Invoker: record.Human,
	}))

	row := rows(t, repo, sha)[0]
	assert.Equal(t, record.Unverified, row.Evidence,
		"the PR said unverified, so the audit says it too")
	assert.Zero(t, row.PRNumber, "a push with no pull request is a publication with no number")
}

func TestTargetOfReadsTheNameAndRefusesToGuessPastIt(t *testing.T) {
	for _, tc := range []struct{ branch, port, want string }{
		{"dockhand/jq-1.8.2", "jq", "1.8.2"},
		{"dockhand/jq-checksums", "jq", "checksums"},
		{"dockhand/jq-rev1", "jq", "rev1"},
		// A subport's note names the subport, and the slug was built
		// from the parent context: the whole slug stands rather than a
		// split that would be wrong.
		{"dockhand/pcre2-10.44", "pcre", "pcre2-10.44"},
		// No port to strip is no reason to invent a boundary. Splitting
		// at the first hyphen here would call "1" the target.
		{"dockhand/jq-1.8.2", "", "jq-1.8.2"},
		{"dockhand/jq", "jq", "jq"},
	} {
		assert.Equal(t, tc.want, targetOf(tc.branch, tc.port), "%s / %s", tc.branch, tc.port)
	}
}

func TestReconcileClosesTheRowWhenThePullRequestMerged(t *testing.T) {
	repo, sha := promotedRepo(t)
	eng := testState(t, repo, &verifytest.Fake{})
	published(t, eng, repo, sha)
	forge := &forgeFake{prs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z",` +
		`"html_url":"https://x/9","merge_commit_sha":"cafe1234"}]`}
	eng.Gh = forge.run

	rep, err := eng.Reconcile(context.Background(), ReconcileOpts{})
	require.NoError(t, err)
	require.Len(t, rep.Branches, 1)
	require.True(t, rep.Branches[0].Retire.Cleaned)

	got := rows(t, repo, sha)
	require.Len(t, got, 2, "the close is a second row, never an edit of the first")
	assert.Equal(t, record.Merged, got[1].Outcome)
	assert.Equal(t, "cafe1234", got[1].MergeSha)
	assert.Equal(t, sha, got[1].MintSha)
	assert.Equal(t, record.Verified, got[1].Evidence, "the publication is carried into the close")
}

func TestTheAuditSurvivesTheDemolitionThatClosedItsRow(t *testing.T) {
	repo, sha := promotedRepo(t)
	eng := testState(t, repo, &verifytest.Fake{})
	published(t, eng, repo, sha)
	runningNote(t, repo, sha, "fake-1")
	forge := &forgeFake{prs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/9"}]`}
	eng.Gh = forge.run

	_, err := eng.Reconcile(context.Background(), ReconcileOpts{})
	require.NoError(t, err)

	ctx := context.Background()
	assert.False(t, repo.HasBranch(ctx, "dockhand/jq-1.8"), "the branch is gone")
	_, verr := repo.NoteRead(ctx, git.VerifyNotesRef, sha)
	require.ErrorIs(t, verr, git.ErrNoNote, "and its verification record with it")
	// The whole reason for a second ref: this outlives both.
	assert.Len(t, rows(t, repo, sha), 2,
		"an audit removed with the branch it describes records nothing worth having")
}

func TestReconcileClosesTheRowOnAnObservedRejection(t *testing.T) {
	repo, sha := promotedRepo(t)
	eng := testState(t, repo, &verifytest.Fake{})
	published(t, eng, repo, sha)
	forge := &forgeFake{prs: `[{"number":9,"state":"closed","merged_at":"","html_url":"https://x/9"}]`}
	eng.Gh = forge.run

	rep, err := eng.Reconcile(context.Background(), ReconcileOpts{})
	require.NoError(t, err)

	require.Len(t, rep.Branches, 1)
	assert.False(t, rep.Branches[0].Retire.Cleaned)
	assert.True(t, repo.HasBranch(context.Background(), "dockhand/jq-1.8"),
		"a rejected branch is never retired; rejection is information")
	got := rows(t, repo, sha)
	require.Len(t, got, 2, "and it is still an outcome, counted like any other")
	assert.Equal(t, record.Rejected, got[1].Outcome)
	assert.Empty(t, got[1].MergeSha)
}

func TestReconcileClosesTheRowWithoutHavingObservedATip(t *testing.T) {
	repo, sha := promotedRepo(t)
	eng := testState(t, repo, &verifytest.Fake{})
	published(t, eng, repo, sha)
	forge := &forgeFake{prs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z",` +
		`"html_url":"https://x/9","merge_commit_sha":"cafe1234"}]`}
	eng.Gh = forge.run

	// The sweep observes no standings, so it holds no tip: the sha the
	// row is keyed by has to be asked for, and asked for before the
	// demolition takes the branch away.
	_, err := eng.Reconcile(context.Background(), ReconcileOpts{RetireOnly: true})
	require.NoError(t, err)

	got := rows(t, repo, sha)
	require.Len(t, got, 2)
	assert.Equal(t, record.Merged, got[1].Outcome)
}

func TestReconcileLeavesAnUnpublishedChangeOutOfTheAudit(t *testing.T) {
	repo, sha := promotedRepo(t)
	eng := testState(t, repo, &verifytest.Fake{})
	forge := &forgeFake{prs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z","html_url":"https://x/9"}]`}
	eng.Gh = forge.run

	rep, err := eng.Reconcile(context.Background(), ReconcileOpts{})
	require.NoError(t, err)

	// A branch promoted by hand, or by an older dockhand, has no
	// opening row. Inventing a close for it would put a change into the
	// audit the audit cannot account for.
	_, err = repo.NoteRead(context.Background(), git.OutcomeNotesRef, sha)
	require.ErrorIs(t, err, git.ErrNoNote)
	require.Len(t, rep.Branches, 1)
	// And it stays invisible: the audit adds no word to a report that
	// had nothing to record.
	require.Len(t, rep.Branches[0].Prose, 2, "the demolition's own two lines and nothing else")
}

func TestReconcileSettlesOnceHoweverOftenItRuns(t *testing.T) {
	repo, sha := promotedRepo(t)
	eng := testState(t, repo, &verifytest.Fake{})
	published(t, eng, repo, sha)
	forge := &forgeFake{prs: `[{"number":9,"state":"closed","merged_at":"","html_url":"https://x/9"}]`}
	eng.Gh = forge.run

	// A rejected branch stands, so every later pass reaches the same
	// verdict about it. `status` on a cron would grow the note forever.
	for range 3 {
		_, err := eng.Reconcile(context.Background(), ReconcileOpts{})
		require.NoError(t, err)
	}

	assert.Len(t, rows(t, repo, sha), 2)
}

// The audit is bookkeeping and never the answer. A row that cannot be
// written is a warning on stderr: by the time it is attempted the
// change is public, and telling a user their promotion failed because a
// note could not be appended is the more misleading of the two answers.
func TestAPublicationRowThatCannotBeWrittenIsAWarning(t *testing.T) {
	repo, sha := engineRepo(t)
	var out, errOut bytes.Buffer
	eng := testEngine(t, repo, &verifytest.Fake{}, &out, &errOut)
	lockNotesRef(t, repo, git.OutcomeNotesRef)

	eng.recordPublication(context.Background(), repo, Publication{
		MintSha: sha, Branch: "dockhand/jq-1.8", Port: "jq",
		PRNumber: 9, Verified: true, Invoker: record.Human,
	})

	assert.Contains(t, errOut.String(), "warning: recording the publication: ")
	assert.Empty(t, out.String(), "stdout carries the pull request's URL, and nothing about the audit")
}

// The closing row's half of the same rule, and the stricter one: the
// warning goes into the branch's prose rather than onto a stream, so
// that `status --json` can route it to stderr instead of breaking the
// document — and it must not displace the answer the reader asked for,
// which is that the pull request merged and the branch is gone.
func TestAnOutcomeRowThatCannotBeWrittenIsAWarningOnTheReport(t *testing.T) {
	repo, sha := promotedRepo(t)
	eng := testState(t, repo, &verifytest.Fake{})
	published(t, eng, repo, sha)
	forge := &forgeFake{prs: `[{"number":9,"state":"closed","merged_at":"2026-09-01T00:00:00Z",` +
		`"html_url":"https://x/9","merge_commit_sha":"cafe1234"}]`}
	eng.Gh = forge.run
	lockNotesRef(t, repo, git.OutcomeNotesRef)

	rep, err := eng.Reconcile(context.Background(), ReconcileOpts{})
	require.NoError(t, err, "bookkeeping never fails the pass")
	require.Len(t, rep.Branches, 1)

	var warned []render.Line
	for _, line := range rep.Branches[0].Prose {
		if strings.HasPrefix(line.Text, "warning: recording the outcome of ") {
			warned = append(warned, line)
		}
	}
	require.Len(t, warned, 1)
	assert.True(t, strings.HasPrefix(warned[0].Text, "warning: recording the outcome of dockhand/jq-1.8: "),
		"the branch is named, because a sweep says this about one branch among many")
	assert.Equal(t, render.ToErr, warned[0].Stream,
		"prose about the audit is never document content: --json routes it to stderr")
	assert.True(t, rep.Branches[0].Retire.Cleaned, "and the merge is still acted on")
}

// A verified row can carry members published without a pass, since the
// dependents are best effort (D24). The audit must be able to tell that
// population from the one where everything built (D26), so the row says
// how many — and says nothing when there were none, so the rows written
// before the field existed and the clean rows written after read alike.
func TestPublishRecordsHowManyMembersWereUnproven(t *testing.T) {
	repo, sha := engineRepo(t)
	var out, errOut bytes.Buffer
	eng := testEngine(t, repo, &verifytest.Fake{}, &out, &errOut)

	require.NoError(t, eng.Publish(context.Background(), repo, Publication{
		MintSha: sha, Branch: "dockhand/libraw-0.22.2", Port: "libraw",
		PRNumber: 9, Verified: true, Invoker: record.Human, Unproven: 2,
	}))
	got := rows(t, repo, sha)
	require.Len(t, got, 1)
	assert.Equal(t, record.Verified, got[0].Evidence, "best effort publishes as verified")
	assert.Equal(t, 2, got[0].Unproven, "and the row says what that verification did not cover")
}

func TestACleanPublicationCarriesNoUnprovenCount(t *testing.T) {
	repo, sha := engineRepo(t)
	var out, errOut bytes.Buffer
	eng := testEngine(t, repo, &verifytest.Fake{}, &out, &errOut)
	published(t, eng, repo, sha)
	assert.Equal(t, 0, rows(t, repo, sha)[0].Unproven)
}
