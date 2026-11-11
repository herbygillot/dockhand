package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// serve's count by its rows: midnight in each of K's four forms, a create
// marked and sent three days later (AJ), a refused second try after an
// unanswered first (AK), the limit lowered (AM), an earlier pull request
// that settles nothing (AN), and a head gone from the fork, kept at the
// first look and released at the second, or back between them (AR)
// (docs/designs/publication-record.md, "serve's count").
func TestServesCountByItsRows(t *testing.T) {
	t.Parallel()
	today := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	yesterday := today.Add(-24 * time.Hour)

	// K. (i) opened before midnight: yesterday's; (ii) held, not tried:
	// today's; (iii) opened after midnight: today's; (iv) tried, nothing
	// found: every day.
	require.False(t, ServeOpening{Opened: today.Add(-time.Minute)}.Counts(today))
	require.True(t, ServeOpening{Opened: today.Add(-time.Minute)}.Counts(yesterday))
	require.True(t, ServeOpening{Holding: true}.Counts(today))
	require.True(t, ServeOpening{Opened: today.Add(time.Minute)}.Counts(today))
	tried := ServeOpening{Tried: yesterday.Add(time.Hour), Unanswered: true, After: 40}
	require.True(t, tried.Counts(yesterday))
	require.True(t, tried.Counts(today))
	require.True(t, tried.Counts(today.Add(72*time.Hour)), "AJ: no length of time settles a tried row")

	// The limit and the place: under a limit of one, a tried row from
	// yesterday holds today's place.
	require.False(t, ServeAdmits([]ServeOpening{tried}, today, 1))
	require.True(t, ServeAdmits([]ServeOpening{{Opened: yesterday.Add(time.Hour)}}, today, 1))

	// AM. Three tried rows, the limit lowered to one: only the oldest's
	// create is sent.
	rows := []ServeOpening{{Tried: today.Add(3 * time.Hour)}, {Tried: today.Add(time.Hour)}, {Tried: today.Add(2 * time.Hour)}}
	require.True(t, ServeMaySend(rows, 1, today, 1))
	require.False(t, ServeMaySend(rows, 0, today, 1))
	require.False(t, ServeMaySend(rows, 2, today, 1))

	// AN. The pull request found at or below After was there before the
	// create; one above it is the row's opening, on GitHub's day.
	_, charged := tried.FoundFor(40, yesterday)
	require.False(t, charged)
	_, charged = tried.FoundFor(41, time.Time{})
	require.False(t, charged, "no creation time leaves the row as it is")
	opened, charged := tried.FoundFor(41, today.Add(2*time.Hour))
	require.True(t, charged)
	require.Equal(t, 41, opened.Number)
	require.True(t, opened.Counts(today))
	require.False(t, opened.Counts(today.Add(24*time.Hour)), "an opening counts on its day alone")

	// AK. A refusal releases a row only where nothing of it is unanswered.
	require.False(t, tried.Refused())
	require.True(t, ServeOpening{Tried: today}.Refused())

	// AR. The head gone from the fork: written down at the first look,
	// released at a second ten minutes on; back in between clears it.
	now := today.Add(5 * time.Hour)
	looked, released := tried.GoneLook(false, now)
	require.False(t, released)
	require.Equal(t, now, looked.Gone)
	_, released = looked.GoneLook(false, now.Add(9*time.Minute))
	require.False(t, released, "not yet ten minutes")
	_, released = looked.GoneLook(false, now.Add(10*time.Minute))
	require.True(t, released)
	back, released := looked.GoneLook(true, now.Add(10*time.Minute))
	require.False(t, released)
	require.True(t, back.Gone.IsZero(), "a head found again clears the mark")
}
