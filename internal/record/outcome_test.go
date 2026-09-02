package record

// The audit row's wire, pinned as bytes. A row is appended to a git
// note and never rewritten, so today's spelling is what every reader
// after today has to keep reading: a field renamed here is a field
// missing from every row already on disk, and nothing in a diff would
// say so.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openRow() OutcomeRow {
	return OutcomeRow{
		MintSha:     "8da38abbbd45c0ffee0000000000000000000000",
		Branch:      "dockhand/jq-1.8.2",
		Port:        "jq",
		Target:      "1.8.2",
		MintedVia:   MintedSingle,
		AskedBy:     Human,
		PublishedBy: Human,
		Evidence:    Verified,
		PRNumber:    42,
		PublishedAt: "2026-09-02T10:00:00Z",
	}
}

// closedRow is the same publication with what became of it.
func closedRow() OutcomeRow {
	row := openRow()
	row.Outcome, row.MergeSha, row.SettledAt = Merged, "cafe1234", "2026-09-09T08:30:00Z"
	return row
}

// pushedWithNoPR is a publication that opened no pull request: the
// fields nothing knew about are simply absent.
func pushedWithNoPR() OutcomeRow {
	return OutcomeRow{
		MintSha: "8da38abbbd45", Branch: "dockhand/jq-1.8.2", MintedVia: MintedSingle,
		AskedBy: Human, PublishedBy: Human, Evidence: Unverified,
		PublishedAt: "2026-09-02T10:00:00Z",
	}
}

func TestOutcomeRowWire(t *testing.T) {
	// Everything these three lines demonstrate is load-bearing. No
	// indentation and no trailing newline, because the note is a log the
	// reader splits on newlines and git completes the final line itself.
	// The keys in declaration order. The closing three absent entirely
	// while the row is open, which is the property Open() reads, and
	// present in their own order once it is closed — carrying the
	// publication's own fields forward, so the last line of a note
	// answers without the lines above it. And omitempty where a value
	// may honestly be unknown: no port, no target, no number. Never on
	// evidence, because unverified is a claim and not an absence.
	for _, tc := range []struct {
		name string
		row  OutcomeRow
		want string
	}{
		{"open", openRow(), `{"mint_sha":"8da38abbbd45c0ffee0000000000000000000000",` +
			`"branch":"dockhand/jq-1.8.2","port":"jq","target":"1.8.2",` +
			`"minted_via":"single","asked_by":"human","published_by":"human",` +
			`"evidence":"verified","pr_number":42,"published_at":"2026-09-02T10:00:00Z"}`},
		{"closed", closedRow(), `{"mint_sha":"8da38abbbd45c0ffee0000000000000000000000",` +
			`"branch":"dockhand/jq-1.8.2","port":"jq","target":"1.8.2",` +
			`"minted_via":"single","asked_by":"human","published_by":"human",` +
			`"evidence":"verified","pr_number":42,"published_at":"2026-09-02T10:00:00Z",` +
			`"outcome":"merged","merge_sha":"cafe1234","settled_at":"2026-09-09T08:30:00Z"}`},
		{"pushed with no PR", pushedWithNoPR(), `{"mint_sha":"8da38abbbd45","branch":"dockhand/jq-1.8.2",` +
			`"minted_via":"single","asked_by":"human","published_by":"human",` +
			`"evidence":"unverified","published_at":"2026-09-02T10:00:00Z"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := EncodeOutcomeRow(tc.row)
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(b))
			assert.NotContains(t, string(b), "\n",
				"a row spanning two lines would end the only property that makes the split exact")
		})
	}
}

func TestDecodeOutcomeRowsStepsOverGitsSeparator(t *testing.T) {
	// The exact bytes `git notes append` leaves behind, measured on git
	// 2.55: the stored note, a blank line, the appended note, and the
	// newline git completes the last line with.
	note := `{"mint_sha":"abc","branch":"dockhand/jq-1.8.2","outcome":""}` + "\n\n" +
		`{"mint_sha":"abc","branch":"dockhand/jq-1.8.2","outcome":"merged"}` + "\n"

	rows, err := DecodeOutcomeRows([]byte(note))
	require.NoError(t, err)

	require.Len(t, rows, 2, "the blank line is git's punctuation and never a row")
	assert.True(t, rows[0].Open())
	assert.Equal(t, Merged, rows[1].Outcome, "oldest first, so the last row is the current one")
	assert.False(t, rows[1].Open())
}

func TestDecodeOutcomeRowsOfAnEmptyNoteIsNoRows(t *testing.T) {
	rows, err := DecodeOutcomeRows(nil)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestDecodeOutcomeRowsNamesTheLineThatWillNotParse(t *testing.T) {
	note := `{"mint_sha":"abc"}` + "\n\n" + "not json\n"

	_, err := DecodeOutcomeRows([]byte(note))

	require.Error(t, err, "an audit with an unreadable line has to say so, not count one publication fewer")
	assert.Contains(t, err.Error(), "line 3",
		"the line number counts the blank separator, because that is what a reader looking at the note sees")
	assert.True(t, strings.HasPrefix(err.Error(), "outcome row on line"))
}

func TestOutcomeRowIsOpenUntilItCarriesAnOutcome(t *testing.T) {
	row := openRow()
	assert.True(t, row.Open())
	row.Outcome = Rejected
	assert.False(t, row.Open(), "rejection is an outcome worth counting, not an absence")
}
