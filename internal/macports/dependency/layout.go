package dependency

import "strings"

// blockLayout reproduces the spacing of an existing declaration block so a
// regenerated block reads like the maintained one: rows whose tokens did not
// change are kept byte for byte, and changed or new rows are placed on the
// same columns. Without a usable sample the caller falls back to plain rows.
type blockLayout struct {
	name string
	// firstOnCommandLine places the first row after the command name, as
	// go2port does, instead of on a continuation line.
	firstOnCommandLine bool
	// indent is the continuation indent for row lines; module lines for Go.
	indent string
	rows   lineLayout
	// Go blocks: key/value lines under each module.
	subIndent string
	fields    lineLayout
	// verbatim maps a row's tokens joined by single spaces to its original
	// text from the first token onward; Go rows join their lines with the
	// continuation and keep their field indentation.
	verbatim map[string]string
}

// column describes one token position. A left-aligned column starts at
// left; a right-aligned one ends at right, never starts before floor, the
// lowest start ever observed, and keeps its width right-floor when the
// previous token runs past the field. gap is the smallest separation seen.
type column struct {
	left, right, floor int
	alignRight         bool
	gap                int
}

type lineLayout []column

type blockLine struct {
	indent string
	tokens []string
	starts []int
	text   string
}

const continuation = " \\\n"

// parseLine splits one physical line into its tokens and their columns,
// dropping the continuation backslash. offset is the column where raw begins.
func parseLine(raw string, offset int) (blockLine, bool) {
	content := strings.TrimRight(strings.TrimSuffix(strings.TrimRight(raw, " \t"), "\\"), " \t")
	trimmed := strings.TrimLeft(content, " \t")
	if trimmed == "" {
		return blockLine{}, false
	}
	l := blockLine{indent: content[:len(content)-len(trimmed)], text: trimmed}
	col := len(l.indent)
	for _, field := range strings.Fields(trimmed) {
		at := strings.Index(content[col:], field) + col
		l.tokens = append(l.tokens, field)
		l.starts = append(l.starts, offset+at)
		col = at + len(field)
	}
	return l, true
}

// inferLayout reads a block's physical lines. It returns nil when the block
// does not have the expected shape, so unusual hand formatting is not
// imitated wrongly.
func inferLayout(kind, text string) *blockLayout {
	lines := strings.Split(text, "\n")
	name, first, _ := strings.Cut(lines[0], " ")
	if name != kind {
		return nil
	}
	layout := &blockLayout{name: kind, verbatim: map[string]string{}}
	var rows []blockLine
	if l, ok := parseLine(first, len(name)+1); ok {
		layout.firstOnCommandLine = true
		l.indent = strings.Repeat(" ", len(name)+1+len(l.indent))
		rows = append(rows, l)
	}
	for _, raw := range lines[1:] {
		if l, ok := parseLine(raw, 0); ok {
			rows = append(rows, l)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	if kind == Go {
		return inferGoLayout(layout, rows)
	}
	width := 3
	if kind == CargoGit {
		width = 5
	}
	for _, l := range rows {
		if len(l.tokens) != width {
			return nil
		}
		layout.verbatim[strings.Join(l.tokens, " ")] = l.text
		if layout.indent == "" || len(l.indent) < len(layout.indent) {
			layout.indent = l.indent
		}
	}
	layout.rows = inferColumns(rows)
	return layout
}

// inferGoLayout reads go2port's shape: a module on its own line, then one
// key/value pair per line beneath it.
func inferGoLayout(layout *blockLayout, rows []blockLine) *blockLayout {
	var fields []blockLine
	var current []string
	var currentText []string
	modules := 0
	flush := func() {
		if len(current) > 0 {
			layout.verbatim[strings.Join(current, " ")] = strings.Join(currentText, continuation)
		}
		current, currentText = nil, nil
	}
	for _, l := range rows {
		switch {
		case len(l.tokens) == 1 && strings.Contains(l.tokens[0], "/"):
			flush()
			modules++
			if layout.indent == "" || len(l.indent) < len(layout.indent) {
				layout.indent = l.indent
			}
			current, currentText = []string{l.tokens[0]}, []string{l.text}
		case len(l.tokens) == 2 && len(current) > 0:
			fields = append(fields, l)
			if layout.subIndent == "" || len(l.indent) < len(layout.subIndent) {
				layout.subIndent = l.indent
			}
			current = append(current, l.tokens...)
			currentText = append(currentText, l.indent+l.text)
		default:
			return nil
		}
	}
	flush()
	if modules == 0 || len(fields) == 0 {
		return nil
	}
	layout.fields = inferColumns(fields)
	return layout
}

// inferColumns derives, for each token position, whether the block aligns
// it on its left or right edge, where, and the smallest gap kept before it
// when a long token pushed the next one along.
func inferColumns(rows []blockLine) lineLayout {
	var layout lineLayout
	for i := 0; ; i++ {
		lefts, rights := map[int]int{}, map[int]int{}
		gap, floor, seen := -1, -1, false
		for _, l := range rows {
			if i >= len(l.tokens) {
				continue
			}
			seen = true
			end := l.starts[i] + len(l.tokens[i])
			lefts[l.starts[i]]++
			rights[end]++
			if floor < 0 || l.starts[i] < floor {
				floor = l.starts[i]
			}
			if i > 0 {
				if g := l.starts[i] - (l.starts[i-1] + len(l.tokens[i-1])); gap < 0 || g < gap {
					gap = g
				}
			}
		}
		if !seen {
			return layout
		}
		left, leftCount := mode(lefts)
		right, rightCount := mode(rights)
		layout = append(layout, column{left: left, right: right, floor: floor, alignRight: rightCount > leftCount, gap: max(gap, 1)})
	}
}

func mode(counts map[int]int) (int, int) {
	best, count := 0, 0
	for value, n := range counts {
		if n > count || n == count && value < best {
			best, count = value, n
		}
	}
	return best, count
}

// render places tokens after prefix on the layout's columns, never closer
// than the observed gap.
func (l lineLayout) render(prefix string, tokens []string) string {
	var b strings.Builder
	b.WriteString(prefix)
	pos := len(prefix)
	for i, token := range tokens {
		if i > 0 {
			target := pos + 1
			if i < len(l) {
				c := l[i]
				if c.alignRight {
					end := max(c.right, pos+c.right-c.floor)
					target = max(c.floor, end-len(token), pos+1)
				} else {
					target = max(c.left, pos+c.gap)
				}
			}
			b.WriteString(strings.Repeat(" ", target-pos))
		}
		b.WriteString(token)
		pos = b.Len()
	}
	return b.String()
}

// format renders the block for the given rows, reusing original text for
// rows whose tokens are unchanged.
func (l *blockLayout) format(rows [][]string) string {
	var lines []string
	for i, row := range rows {
		prefix := l.indent
		if i == 0 && l.firstOnCommandLine {
			prefix = l.name + strings.Repeat(" ", max(1, len(l.indent)-len(l.name)))
		}
		if text, ok := l.verbatim[strings.Join(row, " ")]; ok {
			lines = append(lines, prefix+text)
			continue
		}
		if l.name == Go {
			lines = append(lines, prefix+row[0])
			for j := 1; j+1 < len(row); j += 2 {
				lines = append(lines, l.fields.render(l.subIndent, row[j:j+2]))
			}
			continue
		}
		lines = append(lines, l.rows.render(prefix, row))
	}
	body := strings.Join(lines, continuation)
	if !l.firstOnCommandLine {
		body = l.name + continuation + body
	}
	return body
}
