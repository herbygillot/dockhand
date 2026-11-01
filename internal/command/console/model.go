package console

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/command/view"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/term/screen"
)

// pane is what the pane under the table shows.
type pane int

const (
	// paneEvents is the journal's newest events.
	paneEvents pane = iota
	// paneDetail is the selected branch in detail.
	paneDetail
)

// keptEvents is how many events the pane keeps, more than it ever shows.
const keptEvents = 100

// Model is everything a frame shows, with no terminal: the branches'
// rows, the pane's events or detail, the screen's size, and the row
// selected, kept by its branch across gathers. Frame draws it, so every
// decision of the layout is a test of text.
type Model struct {
	At time.Time
	// Rows are the branches in bucket order (Gathered); All is --all, the
	// merged, closed, and archived among them.
	Rows []view.Row
	All  bool
	// Serve is serve's state as status says it.
	Serve string
	// Problem is why the last gather failed, the rows the last that
	// didn't.
	Problem string
	// Events are the journal's events as lines, newest last.
	Events []string
	// Detail is the selected branch's detail, for the pane that shows it.
	Detail        []string
	Width, Height int
	Selected      model.BranchID
	Pane          pane
}

// Gathered takes the rows a gather found, in bucket order, the done ones
// only with All, keeping the selected row by its branch; where that's gone,
// the row now where it was.
func (m *Model) Gathered(rows []view.Row) {
	at := max(m.index(), 0)
	var kept []view.Row
	for _, row := range rows {
		if row.Bucket != branchstate.BucketDone || m.All {
			kept = append(kept, row)
		}
	}
	slices.SortStableFunc(kept, func(a, b view.Row) int { return int(a.Bucket) - int(b.Bucket) })
	m.Rows = kept
	if m.index() < 0 {
		m.Selected = ""
		if len(kept) > 0 {
			m.Selected = kept[min(at, len(kept)-1)].ID
		}
	}
}

// Heard adds events' lines to the pane's, keeping the newest.
func (m *Model) Heard(lines ...string) {
	m.Events = append(m.Events, lines...)
	if over := len(m.Events) - keptEvents; over > 0 {
		m.Events = slices.Delete(m.Events, 0, over)
	}
}

// Move moves the selection as a key asks: a row, a page, or to the first
// or last; it reports whether the selection moved.
func (m *Model) Move(key screen.Key) bool {
	if len(m.Rows) == 0 {
		return false
	}
	at, page := max(m.index(), 0), m.layout().rows
	to := at
	switch key.Code {
	case screen.Up:
		to--
	case screen.Down:
		to++
	case screen.PageUp:
		to -= page
	case screen.PageDown:
		to += page
	case screen.Home:
		to = 0
	case screen.End:
		to = len(m.Rows) - 1
	default:
		return false
	}
	to = min(max(to, 0), len(m.Rows)-1)
	m.Selected = m.Rows[to].ID
	return to != at
}

// Row is the selected row, where there is one.
func (m Model) Row() (view.Row, bool) {
	if i := m.index(); i >= 0 {
		return m.Rows[i], true
	}
	return view.Row{}, false
}

func (m Model) index() int {
	return slices.IndexFunc(m.Rows, func(row view.Row) bool { return row.ID == m.Selected })
}

// layout is how a frame's height is shared: the header, the column
// titles, the pane's title, and the keys take a line each; the pane a third
// of the height, three lines to eight; and the table's rows the rest, at
// least one, the pane giving way on a short screen.
type layout struct {
	rows, pane int
}

func (m Model) layout() layout {
	panes := min(max(m.Height/3, 3), 8)
	rows := m.Height - 4 - panes
	if rows < 1 {
		panes = max(panes+rows-1, 0)
		rows = 1
	}
	return layout{rows: rows, pane: panes}
}

// page is the selection's page of the rows, from 0, and how many pages
// there are.
func (m Model) page() (page, pages int) {
	size := m.layout().rows
	pages = max((len(m.Rows)+size-1)/size, 1)
	return max(m.index(), 0) / size, pages
}

// keys is the last line: what the keys do.
func (m Model) keys() string {
	enter := "Enter detail"
	if m.Pane == paneDetail {
		enter = "Enter events"
	}
	return "↑↓ select · PgUp PgDn page · " + enter + " · l logs · c check · t tidy · s submit · r refresh · q quit"
}

// Frame is the model drawn as the screen's lines, exactly its height.
func Frame(m Model) []screen.Line {
	lay := m.layout()
	page, pages := m.page()
	var lines []screen.Line
	add := func(text string, reverse bool) {
		lines = append(lines, screen.Line{Text: cut(text, m.Width), Reverse: reverse})
	}

	counts := map[branchstate.Bucket]int{}
	for _, row := range m.Rows {
		counts[row.Bucket]++
	}
	header := fmt.Sprintf("dockhand console · %s · Needs you %d · Working %d · Waiting %d", m.At.Local().Format("15:04:05"), counts[branchstate.BucketNeedsYou], counts[branchstate.BucketWorking], counts[branchstate.BucketWaiting])
	if m.All {
		header += fmt.Sprintf(" · Done %d", counts[branchstate.BucketDone])
	}
	header += fmt.Sprintf(" · page %d/%d", page+1, pages)
	if m.Serve != "" {
		header += " · " + m.Serve
	}
	add(header, false)

	widths := columnWidths(m.Rows, m.Width)
	add("  "+cells(titles, widths), false)
	shown := 0
	if len(m.Rows) == 0 {
		empty := "No open branches. Run dockhand update <port> to start a port update, or dockhand start <name> for other work."
		if m.All {
			empty = "No branches. Run dockhand update <port> to start a port update, or dockhand start <name> for other work."
		}
		add("  "+empty, false)
		shown = 1
	}
	from := page * lay.rows
	for _, row := range m.Rows[min(from, len(m.Rows)):min(from+lay.rows, len(m.Rows))] {
		add(row.Mark+" "+cells([5]string{row.Branch, row.Ports, row.State, row.PR, row.Next}, widths), row.ID == m.Selected)
		shown++
	}
	for ; shown < lay.rows; shown++ {
		add("", false)
	}

	add(m.paneTitle(), false)
	for _, line := range m.paneLines(lay.pane) {
		add(line, false)
	}
	add(m.keys(), false)
	return lines[:min(len(lines), max(m.Height, 0))]
}

// paneTitle is the pane's rule, saying what it shows.
func (m Model) paneTitle() string {
	title := "── Events "
	if row, ok := m.Row(); ok && m.Pane == paneDetail {
		title = "── " + row.Branch + " "
	}
	return title + strings.Repeat("─", max(m.Width-utf8.RuneCountInString(title), 0))
}

// paneLines are the pane's lines, exactly height of them: a gather's
// problem first, then the newest events, oldest at the top, or the
// selected branch's detail from its top, its last line counting what's
// cut.
func (m Model) paneLines(height int) []string {
	var lines []string
	if m.Problem != "" {
		lines = append(lines, view.Look+" "+m.Problem)
	}
	room := height - len(lines)
	switch {
	case m.Pane == paneDetail && len(m.Detail) > room && room > 0:
		lines = append(lines, m.Detail[:room-1]...)
		lines = append(lines, fmt.Sprintf("+%d more; dockhand status %s shows them all", len(m.Detail)-room+1, m.selectedName()))
	case m.Pane == paneDetail:
		lines = append(lines, m.Detail...)
	case len(m.Events) == 0:
		lines = append(lines, "Watching for changes; nothing has happened since the console opened.")
	default:
		lines = append(lines, m.Events[max(len(m.Events)-room, 0):]...)
	}
	lines = lines[:min(len(lines), max(height, 0))]
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func (m Model) selectedName() string {
	row, _ := m.Row()
	return row.Name
}

// titles are the table's columns.
var titles = [5]string{"BRANCH", "PORTS", "STATE", "PR", "NEXT"}

// floors are how narrow each fixed column may be made, and NEXT's the
// least it's given before they're made so.
var floors = [5]int{12, 8, 12, 6, 16}

// columnWidths fits the columns to width as v2's table did: each as wide
// as its widest cell; where they don't fit, the fixed columns shrink
// toward their floors, the widest first, and NEXT takes the rest.
func columnWidths(rows []view.Row, width int) [5]int {
	var widths [5]int
	for i, title := range titles {
		widths[i] = utf8.RuneCountInString(title)
	}
	for _, row := range rows {
		for i, cell := range [5]string{row.Branch, row.Ports, row.State, row.PR, row.Next} {
			widths[i] = max(widths[i], utf8.RuneCountInString(cell))
		}
	}
	// The mark's two columns, and two between each pair of columns.
	room := width - 2 - 2*4
	next := min(widths[4], floors[4])
	for widths[0]+widths[1]+widths[2]+widths[3]+next > room {
		widest := -1
		for i := range 4 {
			if widths[i] > floors[i] && (widest < 0 || widths[i] > widths[widest]) {
				widest = i
			}
		}
		if widest < 0 {
			break
		}
		widths[widest]--
	}
	widths[4] = max(room-widths[0]-widths[1]-widths[2]-widths[3], 0)
	return widths
}

// cells sets a row's cells in their columns, each cut with an ellipsis
// where it's wider, and the last unpadded.
func cells(row [5]string, widths [5]int) string {
	var b strings.Builder
	for i, cell := range row {
		if i > 0 {
			b.WriteString("  ")
		}
		cell = cut(cell, widths[i])
		b.WriteString(cell)
		if i < len(row)-1 {
			b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell)))
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// cut is text at most width runes, its end said with "…" where it's cut.
func cut(text string, width int) string {
	if utf8.RuneCountInString(text) <= width {
		return text
	}
	if width <= 0 {
		return ""
	}
	return string([]rune(text)[:width-1]) + "…"
}
