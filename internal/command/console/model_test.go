package console

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term/screen"
)

// update writes the golden frames anew: go test ./internal/command/console
// -run TestGoldenFrames -update.
var update = flag.Bool("update", false, "write the golden frames anew")

// longNext is a Next of 120 runes, which no column fits.
var longNext = "dockhand submit --branch port00-update --note " + strings.Repeat("é", 120-len([]rune("dockhand submit --branch port00-update --note ")))

// rows are n branches' rows, a third needing you, a third under way, and
// a third waiting on reviewers, given in no order, the first with a Next
// no column fits.
func rows(n int) []view.Row {
	var made []view.Row
	for i := range n {
		row := view.Row{ID: model.BranchID(fmt.Sprintf("b%02d", i)), Name: fmt.Sprintf("port%02d-update", i), Ports: fmt.Sprintf("port%02d 1.%d.0", i, i)}
		row.Branch = row.Name
		switch i % 3 {
		case 0:
			row.Bucket, row.Mark, row.Work, row.Check, row.PR = branchstate.BucketNeedsYou, view.Broken, "1 commit", "1 failed, 0 passed", "—"
			row.Next = fmt.Sprintf("dockhand logs check-%d %s", i+1, row.Name[:6])
		case 1:
			row.Bucket, row.Mark, row.Work, row.Check, row.PR = branchstate.BucketWorking, view.Working, "1 commit", "running (check-9)", "—"
			row.Next = "dockhand wait check-9"
		case 2:
			row.Bucket, row.Mark, row.Work, row.Check, row.PR = branchstate.BucketWaiting, view.Unmarked, "1 commit", "passed for this commit", fmt.Sprintf("#%d approved, CI ✓", 30000+i)
		}
		row.State = row.Work + " · " + row.Check
		made = append(made, row)
	}
	if n > 0 {
		made[0].Next = longNext
	}
	// Given waiting first, as a gather may give them.
	for i, j := 0, len(made)-1; i < j; i, j = i+1, j-1 {
		made[i], made[j] = made[j], made[i]
	}
	return made
}

// drawn is a frame as a golden file has it: each line between bars, a
// reversed one between > and <, every line the width.
func drawn(frame []screen.Line, width int) string {
	var b strings.Builder
	for _, line := range frame {
		left, right := "|", "|"
		if line.Reverse {
			left, right = ">", "<"
		}
		b.WriteString(left + screen.Fit(line.Text, width) + right + "\n")
	}
	return b.String()
}

// sample is a model at a size with n branches, events heard, and serve
// running, at a fixed time.
func sample(width, height, n int) Model {
	m := Model{Width: width, Height: height, At: time.Date(2026, 10, 31, 15, 4, 5, 0, time.Local), Serve: "serve: running (pid 4711) · queue: 1 run"}
	m.Gathered(rows(n))
	for i := range 12 {
		m.Heard(fmt.Sprintf("15:0%d:%02d  port%02d-update  check-%d passed", i/6, i*5%60, i, i+1))
	}
	return m
}

// Every layout decision is a frame: at 80×24, 120×40, and 60×15, with no
// branches, three, and forty, the selection on the first row and then
// paged to the last, the columns fitted to the width.
func TestGoldenFrames(t *testing.T) {
	t.Parallel()
	for _, size := range [][2]int{{80, 24}, {120, 40}, {60, 15}} {
		for _, n := range []int{0, 3, 40} {
			for _, last := range []bool{false, true} {
				if last && n == 0 {
					continue
				}
				name := fmt.Sprintf("%dx%d-%d", size[0], size[1], n)
				if last {
					name += "-last"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					m := sample(size[0], size[1], n)
					if last {
						m.Move(screen.Key{Code: screen.End})
					}
					frame := Frame(m)
					require.Len(t, frame, size[1], "a frame is the screen's height")
					got := drawn(frame, size[0])
					path := filepath.Join("testdata", name+".golden")
					if *update {
						require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
					}
					want, err := os.ReadFile(path)
					require.NoError(t, err)
					require.Equal(t, string(want), got)
				})
			}
		}
	}
}

// The rows come in bucket order, needs-you first, the done ones only with
// --all; a page is the table's rows, and the header says which.
func TestPagingAndSelection(t *testing.T) {
	t.Parallel()
	m := sample(80, 24, 40)
	require.Equal(t, 12, m.layout().rows, "24 lines: the header, titles, pane's title, keys, and eight of pane leave twelve")
	require.Equal(t, 8, m.layout().pane)
	require.Equal(t, branchstate.BucketNeedsYou, m.Rows[0].Bucket)
	require.Equal(t, branchstate.BucketWaiting, m.Rows[39].Bucket)
	require.Equal(t, m.Rows[0].ID, m.Selected, "the first row is selected at first")

	require.False(t, m.Move(screen.Key{Code: screen.Up}), "nothing above the first")
	require.True(t, m.Move(screen.Key{Code: screen.Down}))
	require.Equal(t, m.Rows[1].ID, m.Selected)
	m.Move(screen.Key{Code: screen.PageDown})
	require.Equal(t, m.Rows[13].ID, m.Selected, "a page is the table's rows")
	page, pages := m.page()
	require.Equal(t, [2]int{1, 4}, [2]int{page, pages})
	m.Move(screen.Key{Code: screen.End})
	page, _ = m.page()
	require.Equal(t, 3, page)
	m.Move(screen.Key{Code: screen.PageDown})
	require.Equal(t, m.Rows[39].ID, m.Selected, "nothing past the last")
	m.Move(screen.Key{Code: screen.Home})
	require.Equal(t, m.Rows[0].ID, m.Selected)
	m.Move(screen.Key{Code: screen.PageUp})
	require.Equal(t, m.Rows[0].ID, m.Selected)
	require.False(t, m.Move(screen.Key{Code: screen.Rune, Rune: 'x'}), "another key moves nothing")

	short := sample(60, 6, 3)
	require.Equal(t, layout{rows: 1, pane: 1}, short.layout(), "on a short screen the pane gives way, and a row stays")

	done := rows(3)
	done[0].Bucket = branchstate.BucketDone
	var open Model
	open.Gathered(done)
	require.Len(t, open.Rows, 2, "without --all, no done branch")
	every := Model{All: true}
	every.Gathered(done)
	require.Len(t, every.Rows, 3)
	require.Equal(t, branchstate.BucketDone, every.Rows[2].Bucket)
}

// The selection is the branch's, wherever a gather puts it; where the
// branch is gone, the row now where it was.
func TestTheSelectionIsKeptByBranch(t *testing.T) {
	t.Parallel()
	m := sample(80, 24, 6)
	m.Move(screen.Key{Code: screen.Down})
	m.Move(screen.Key{Code: screen.Down})
	chosen := m.Selected
	reordered := rows(6)
	for i := range reordered {
		if reordered[i].ID == chosen {
			reordered[i].Bucket = branchstate.BucketWaiting
		}
	}
	m.Gathered(reordered)
	row, ok := m.Row()
	require.True(t, ok)
	require.Equal(t, chosen, row.ID, "kept by its branch, not its place")
	require.NotEqual(t, chosen, m.Rows[2].ID, "which moved")

	at := m.index()
	var without []view.Row
	for _, row := range reordered {
		if row.ID != chosen {
			without = append(without, row)
		}
	}
	m.Gathered(without)
	require.Equal(t, m.Rows[min(at, len(m.Rows)-1)].ID, m.Selected, "gone, the row now where it was")
	m.Gathered(nil)
	_, ok = m.Row()
	require.False(t, ok, "and none with no rows")
}

// Events are added at the bottom of the pane, the oldest cut, and the
// pane keeps no more than keptEvents; the detail is cut at the pane, its
// last line counting the rest.
func TestThePaneTakesEventsAndDetail(t *testing.T) {
	t.Parallel()
	m := sample(80, 24, 3)
	lines := m.paneLines(3)
	require.Equal(t, []string{"15:01:45  port09-update  check-10 passed", "15:01:50  port10-update  check-11 passed", "15:01:55  port11-update  check-12 passed"}, lines, "the newest, oldest at the top")
	for i := range keptEvents + 5 {
		m.Heard(fmt.Sprint(i))
	}
	require.Len(t, m.Events, keptEvents)
	require.Equal(t, fmt.Sprint(keptEvents+4), m.Events[keptEvents-1])

	m.Pane = paneDetail
	m.Detail = []string{"a", "b", "c", "d", "e"}
	require.Equal(t, []string{"a", "b", "+3 more; dockhand status port00-update shows them all"}, m.paneLines(3))
	require.Equal(t, []string{"a", "b", "c", "d", "e", ""}, m.paneLines(6), "a pane is its height")
	m.Problem = "status: the database is locked"
	require.Equal(t, "! status: the database is locked", m.paneLines(3)[0], "a gather's problem first")
	require.Contains(t, Frame(m)[14].Text, "── port00-update ─", "the pane's title names the branch")
	require.Contains(t, Frame(m)[23].Text, "Enter events")
}

// A column wider than its share is cut with an ellipsis by runes, the
// fixed ones toward their floors, the widest first, and NEXT takes the
// rest.
func TestColumnsFitTheWidth(t *testing.T) {
	t.Parallel()
	wide := []view.Row{{Branch: strings.Repeat("b", 30), Ports: strings.Repeat("p", 20), State: strings.Repeat("s", 40), PR: "#1", Next: longNext}}
	require.Equal(t, [5]int{30, 20, 40, 2, 150 - 10 - 92}, columnWidths(wide, 150))
	require.Equal(t, [5]int{19, 19, 20, 2, 16}, columnWidths(wide, 86), "the widest gives first, down to its floor")
	require.Equal(t, [5]int{12, 8, 12, 2, 0}, columnWidths(wide, 40), "all at their floors, NEXT has what's left")
	require.Equal(t, "ééé…", cut("ééééé", 4))
	require.Equal(t, "éééé", cut("éééé", 4))
	require.Empty(t, cut("abc", 0))
}
