package tui

import (
	"bufio"
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/workflow/view"
	"io"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/herbygillot/dockhand/internal/workflow"
)

// Options wires the table to the rest of dockhand. Poll reads a snapshot;
// Run executes one dockhand verb in-process, writing its output to out;
// Open shows a URL or file to the person, typically in the browser;
// Processor, when set, advances the repository's work for as long as the
// table is open, reporting through say into the message strip.
type Options struct {
	Poll      func(context.Context) (workflow.Overview, error)
	Run       func(ctx context.Context, args []string, out io.Writer) error
	Open      func(target string) error
	Processor func(ctx context.Context, say func(scope, text string)) error
	// Verbs turns a key's verb on a row into a command. The command tree
	// supplies it, so the verb and flag spellings live where they are defined.
	Verbs Verbs
	// ShowRetired starts the table with merged, closed, and abandoned rows
	// visible; the h key toggles them.
	ShowRetired bool
	Interval    time.Duration
	// Messages is how many strip lines stay visible.
	Messages int
}

// Verbs is how the table maps a verb on a row onto a dockhand command.
type Verbs struct {
	// Args builds the command for a verb on a row, or says why it cannot run.
	Args func(verb string, row view.Contribution) (args []string, problem string)
	// Confirms says whether a verb asks before it runs.
	Confirms func(verb string) bool
}

// Run shows the table until the person quits or the context ends. The
// processor, if any, stops with the table.
func Run(ctx context.Context, in io.Reader, out io.Writer, options Options) error {
	model := newModel(options)
	program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out))
	model.runner.send = program.Send
	_, err := program.Run()
	model.stopProcessor()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

type snapshotMsg struct {
	overview workflow.Overview
	err      error
}
type tickMsg time.Time
type lineMsg struct{ port, text string }
type doneMsg struct {
	port, verb string
	err        error
}
type processorMsg struct{ err error }

// runner executes verbs in the background and streams their output lines
// back into the program as messages.
type runner struct {
	mu      sync.Mutex
	send    func(tea.Msg)
	running map[string]string
}

// pending is a verb waiting for the person's confirmation.
type pending struct {
	verb string
	args []string
	port string
}

type model struct {
	options Options
	runner  *runner
	rows    []view.Contribution
	// showRetired includes retired rows; hidden counts those left out.
	showRetired bool
	readAt      time.Time
	now         time.Time
	err         error
	cursor      int
	expanded    bool
	messages    []string
	confirm     *pending
	width       int
	height      int
	// processing is set while the processor runs; stop ends it and done
	// closes when it has returned.
	processing bool
	stop       context.CancelFunc
	done       chan struct{}
}

func newModel(options Options) *model {
	if options.Interval <= 0 {
		options.Interval = 2 * time.Second
	}
	if options.Messages <= 0 {
		options.Messages = 5
	}
	return &model{options: options, showRetired: options.ShowRetired, runner: &runner{running: map[string]string{}}, width: 100, height: 30, now: time.Now()}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.poll(), m.tick(), m.startProcessor())
}

// startProcessor runs the processor in the background for the life of the
// table; its reports and its end land in the strip.
func (m *model) startProcessor() tea.Cmd {
	if m.options.Processor == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.processing, m.stop, m.done = true, cancel, make(chan struct{})
	processor, send, done := m.options.Processor, m.runner.send, m.done
	return func() tea.Msg {
		defer close(done)
		err := processor(ctx, func(scope, text string) {
			if send != nil {
				send(lineMsg{port: scope, text: text})
			}
		})
		if ctx.Err() != nil {
			return nil
		}
		return processorMsg{err: err}
	}
}

func (m *model) stopProcessor() {
	if m.stop == nil {
		return
	}
	m.stop()
	<-m.done
	m.stop, m.processing = nil, false
}

func (m *model) poll() tea.Cmd {
	return func() tea.Msg {
		overview, err := m.options.Poll(context.Background())
		return snapshotMsg{overview: overview, err: err}
	}
}

func (m *model) tick() tea.Cmd {
	return tea.Tick(m.options.Interval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 0 && msg.Height > 0 {
			m.width, m.height = msg.Width, msg.Height
		}
	case snapshotMsg:
		m.now = time.Now()
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.rows = msg.overview.Contributions
		m.readAt = msg.overview.ReadAt
		m.clampCursor()
	case tickMsg:
		m.now = time.Time(msg)
		return m, tea.Batch(m.poll(), m.tick())
	case lineMsg:
		m.say(msg.port, msg.text)
	case doneMsg:
		m.runner.finish(msg.port)
		if msg.err != nil {
			m.say(msg.port, msg.verb+" failed: "+msg.err.Error())
		} else {
			m.say(msg.port, msg.verb+" started")
		}
		return m, m.poll()
	case processorMsg:
		m.processing = false
		if msg.err != nil {
			m.say("", "processing stopped: "+msg.err.Error())
		} else {
			m.say("", "processing stopped")
		}
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.confirm != nil {
		switch key {
		case "y", "Y":
			action := *m.confirm
			m.confirm = nil
			return m, m.start(action)
		case "n", "N", "esc", "q":
			m.say(m.confirm.port, m.confirm.verb+" not started")
			m.confirm = nil
		}
		return m, nil
	}
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.visible())-1 {
			m.cursor++
		}
	case "enter", " ", "space":
		m.expanded = !m.expanded
	case "h":
		m.showRetired = !m.showRetired
		m.clampCursor()
	case "o":
		m.open("PR", m.selected().PullRequest)
	case "l":
		m.open("log", m.selected().Log)
	case "b":
		// Retry runs the row's own stopped work again. Which verb that is
		// belongs to the projection: a contribution dockhand prepared is
		// retried by its preparing action, one adopted from someone's own
		// branch by verifying it.
		row := m.selected()
		if row.Port == "" {
			return m, nil
		}
		if row.Retry == "" {
			m.say(row.Port, "nothing stopped to retry")
			return m, nil
		}
		return m, m.verb(row.Retry)
	case "c", "v", "p", "r", "a":
		verb := map[string]string{"c": "cancel", "v": "verify", "p": "publish", "r": "refresh", "a": "abandon"}[key]
		return m, m.verb(verb)
	}
	return m, nil
}

// visible is the rows the table shows: all of them, or only those not retired.
func (m *model) visible() []view.Contribution {
	if m.showRetired {
		return m.rows
	}
	return view.Current(m.rows)
}

func (m *model) hidden() int { return len(m.rows) - len(m.visible()) }

func (m *model) clampCursor() {
	if n := len(m.visible()); m.cursor >= n {
		m.cursor = max(n-1, 0)
	}
}

func (m *model) selected() view.Contribution {
	if rows := m.visible(); m.cursor < len(rows) {
		return rows[m.cursor]
	}
	return view.Contribution{}
}

func (m *model) open(what, target string) {
	row := m.selected()
	if target == "" {
		m.say(row.Port, "no "+what+" recorded")
		return
	}
	if m.options.Open == nil {
		m.say(row.Port, what+": "+target)
		return
	}
	if err := m.options.Open(target); err != nil {
		m.say(row.Port, "opening "+what+" failed: "+err.Error())
		return
	}
	m.say(row.Port, "opened "+target)
}

// verb maps a key onto a dockhand command for the selected row through the
// verbs the command tree supplied; those that cost minutes, push, or discard
// ask first.
func (m *model) verb(verb string) tea.Cmd {
	row := m.selected()
	if row.Port == "" {
		return nil
	}
	if m.options.Verbs.Args == nil {
		m.say(row.Port, "no commands are wired to this table")
		return nil
	}
	args, problem := m.options.Verbs.Args(verb, row)
	if problem != "" {
		m.say(row.Port, problem)
		return nil
	}
	if running := m.runner.current(row.Port); running != "" {
		m.say(row.Port, running+" is still running")
		return nil
	}
	action := pending{verb: verb, args: args, port: row.Port}
	if m.options.Verbs.Confirms != nil && m.options.Verbs.Confirms(verb) {
		m.confirm = &action
		return nil
	}
	return m.start(action)
}

func (m *model) start(action pending) tea.Cmd {
	if !m.runner.begin(action.port, action.verb) {
		m.say(action.port, m.runner.current(action.port)+" is still running")
		return nil
	}
	m.say(action.port, "running dockhand "+strings.Join(action.args, " "))
	run, send := m.options.Run, m.runner.send
	return func() tea.Msg {
		reader, writer := io.Pipe()
		go func() {
			scanner := bufio.NewScanner(reader)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				if text := strings.TrimSpace(scanner.Text()); text != "" && send != nil {
					send(lineMsg{port: action.port, text: text})
				}
			}
		}()
		err := run(context.Background(), action.args, writer)
		writer.Close()
		return doneMsg{port: action.port, verb: action.verb, err: err}
	}
}

func (r *runner) begin(port, verb string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, busy := r.running[port]; busy {
		return false
	}
	r.running[port] = verb
	return true
}
func (r *runner) finish(port string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.running, port)
}
func (r *runner) current(port string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running[port]
}

func (m *model) say(port, text string) {
	if port != "" {
		text = port + ": " + text
	}
	m.messages = append(m.messages, text)
	if len(m.messages) > m.options.Messages {
		m.messages = m.messages[len(m.messages)-m.options.Messages:]
	}
}

var (
	headerStyle   = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	faintStyle    = lipgloss.NewStyle().Faint(true)
	detailStyle   = lipgloss.NewStyle().Faint(true).PaddingLeft(4)
)

type column struct {
	title string
	width int
}

func (m *model) View() string {
	var b strings.Builder
	age := "not yet read"
	if !m.readAt.IsZero() {
		age = fmt.Sprintf("read %s ago", m.now.Sub(m.readAt).Truncate(time.Second))
	}
	title := "dockhand status"
	if m.processing {
		title += " (processing)"
	}
	if hidden := m.hidden(); hidden > 0 {
		age += fmt.Sprintf("  %d retired hidden (h shows)", hidden)
	}
	fmt.Fprintf(&b, "%s  %s\n", headerStyle.Render(title), faintStyle.Render(age))
	if m.err != nil {
		fmt.Fprintf(&b, "%s\n", "status unavailable: "+m.err.Error())
	}
	if len(m.visible()) == 0 {
		b.WriteString("No open contributions.\n")
	} else {
		m.table(&b)
	}
	b.WriteString("\n")
	for _, message := range m.messages {
		b.WriteString(truncate(message, m.width) + "\n")
	}
	if m.confirm != nil {
		fmt.Fprintf(&b, "%s", headerStyle.Render(fmt.Sprintf("Run dockhand %s for %s? y/n", m.confirm.verb, m.confirm.port)))
	} else {
		b.WriteString(faintStyle.Render("↑/↓ select  enter expand  h history  b retry  v verify  p publish  r refresh  c cancel  a abandon  o open PR  l log  q quit"))
	}
	return b.String()
}

func (m *model) table(b *strings.Builder) {
	rows := m.visible()
	columns := []column{{"PORT", 4}, {"CHANGE", 6}, {"PHASE", 5}, {"STATE", 5}, {"NEXT", 4}}
	for _, row := range rows {
		for i, value := range []string{row.Port, row.Change, row.Phase, row.State, row.Next} {
			columns[i].width = max(columns[i].width, len([]rune(value)))
		}
	}
	fit(columns, m.width)
	var titles []string
	for _, c := range columns {
		titles = append(titles, pad(c.title, c.width))
	}
	b.WriteString(headerStyle.Render(strings.Join(titles, "  ")) + "\n")
	first, last := m.window()
	for i := first; i < last; i++ {
		row := rows[i]
		var cells []string
		for j, value := range []string{row.Port, row.Change, row.Phase, row.State, row.Next} {
			cells = append(cells, pad(truncate(value, columns[j].width), columns[j].width))
		}
		text := strings.Join(cells, "  ")
		if i == m.cursor {
			text = selectedStyle.Render(text)
		}
		b.WriteString(text + "\n")
		if i == m.cursor && m.expanded {
			for _, line := range expansion(row) {
				b.WriteString(detailStyle.Render(truncate(line, m.width-4)) + "\n")
			}
		}
	}
}

// fit shrinks columns until the table fits the width: the widest of the
// fixed columns gives way first, down to eight cells, and NEXT keeps at
// least a readable minimum.
func fit(columns []column, width int) {
	const gap, floor, nextFloor = 2, 8, 16
	total := func() int {
		sum := gap * (len(columns) - 1)
		for _, c := range columns {
			sum += c.width
		}
		return sum
	}
	last := len(columns) - 1
	for total() > width {
		widest := -1
		for i := 0; i < last; i++ {
			if columns[i].width > floor && (widest < 0 || columns[i].width > columns[widest].width) {
				widest = i
			}
		}
		if widest >= 0 && columns[last].width <= nextFloor || widest >= 0 && columns[widest].width > columns[last].width {
			columns[widest].width--
			continue
		}
		if columns[last].width > floor {
			columns[last].width--
			continue
		}
		break
	}
}

// window keeps the cursor visible when there are more rows than lines.
func (m *model) window() (int, int) {
	available := max(m.height-4-m.options.Messages-2, 3)
	if m.expanded {
		available = max(available-len(expansion(m.selected())), 1)
	}
	count := len(m.visible())
	if count <= available {
		return 0, count
	}
	first := max(m.cursor-available/2, 0)
	last := min(first+available, count)
	return max(last-available, 0), last
}

// expansion lists what a selected row hides: identifiers, targets, the PR,
// the active job, and the history.
func expansion(row view.Contribution) []string {
	var lines []string
	if row.Branch != "" {
		lines = append(lines, "branch: "+row.Branch)
	}
	if len(row.Targets) > 1 {
		lines = append(lines, "targets: "+strings.Join(row.Targets, ", "))
	}
	if row.ChangeID != "" {
		lines = append(lines, "contribution: "+string(row.ChangeID))
	}
	if row.PullRequest != "" {
		lines = append(lines, "PR: "+row.PullRequest)
	}
	if row.Log != "" {
		lines = append(lines, "log: "+row.Log)
	}
	if row.Active != nil {
		text := fmt.Sprintf("active: %s %s", row.Active.Action, row.Active.JobID)
		if row.Active.Detail != "" {
			text += "; " + row.Active.Detail
		}
		lines = append(lines, text)
	} else if row.Detail != "" {
		lines = append(lines, "detail: "+row.Detail)
	}
	for _, job := range row.History {
		text := fmt.Sprintf("%s %s: %s", job.AcceptedAt.Local().Format("Jan 2 15:04"), job.Action, job.State)
		if job.Detail != "" {
			text += "; " + job.Detail
		}
		lines = append(lines, text+" ("+string(job.JobID)+")")
	}
	for _, earlier := range row.Earlier {
		text := fmt.Sprintf("earlier: %s; %s; %s", earlier.Change, earlier.State, earlier.Next)
		if earlier.PullRequest != "" {
			text += "; " + earlier.PullRequest
		}
		lines = append(lines, text)
	}
	return lines
}

func pad(s string, width int) string {
	if n := len([]rune(s)); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

func truncate(s string, width int) string {
	runes := []rune(s)
	if width <= 0 || len(runes) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}
