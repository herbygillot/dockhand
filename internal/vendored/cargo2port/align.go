package cargo2port

import "strings"

// Layout is a cargo.crates column layout, in the generator's own
// vocabulary: these are cargo2port's --align values, because the choice
// this package makes is which layout to ask the tool for, never how to
// lay a block out itself.
type Layout string

const (
	// LayoutJustify right-aligns versions against a common column —
	// the tree's convention, 224 blocks against 5 when surveyed, and
	// the default whenever a block does not say otherwise.
	LayoutJustify Layout = "justify"
	// LayoutMaxlen pads names to the longest and leaves versions
	// left-aligned.
	LayoutMaxlen Layout = "maxlen"
	// LayoutMultiline separates fields with single spaces.
	LayoutMultiline Layout = "multiline"
	// LayoutRagged is a block in none of the tool's layouts — tabs,
	// hand edits, drift. It cannot be reproduced, so regeneration falls
	// back to the tree's convention.
	LayoutRagged Layout = "ragged"
)

// alignFlag is the generator flag a layout maps back to.
func (l Layout) alignFlag() string {
	if l == LayoutRagged {
		l = LayoutJustify
	}
	return "--align=" + string(l)
}

// crateLine is one measured entry: where its fields start and end, in
// byte columns.
type crateLine struct {
	vStart, vEnd, sStart int
	sep1, sep2           int // widths of the two field separators
}

// Alignment assesses which of the generator's layouts an existing block
// is written in, so regeneration can ask for the same one and keep the
// diff to the crates that actually moved. The classification reads
// column geometry only — which edge holds still across lines — and
// never interprets a crate.
//
// A block too small to have geometry (one crate), or with uniform
// field widths that make the layouts indistinguishable, answers
// justify: the tree's convention, and harmlessly identical output
// either way in the indistinguishable case.
func Alignment(block string) Layout {
	var lines []crateLine
	first := true
	for raw := range strings.Lines(block) {
		line := strings.TrimRight(raw, "\n")
		line = strings.TrimSuffix(strings.TrimRight(line, " \t"), "\\")
		line = strings.TrimRight(line, " ")
		if first {
			// The option name's own line carries no crate geometry.
			first = false
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(line, "\t") {
			// Tab stops make byte columns and visual columns two
			// different questions; nothing the tool emits uses them.
			return LayoutRagged
		}
		m, ok := measure(line)
		if !ok {
			return LayoutRagged
		}
		lines = append(lines, m)
	}
	if len(lines) < 2 {
		return LayoutJustify
	}

	singleSpaced := true
	for _, l := range lines {
		if l.sep1 != 1 || l.sep2 != 1 {
			singleSpaced = false
			break
		}
	}
	// A strictly uniform left version edge with varying right edges is
	// maxlen definitively, and must be read before the justify test:
	// a maxlen block whose versions are mostly the same width would
	// otherwise pass justify's modal right edge, with the odd widths
	// excused as overflow.
	uniformStart, varyingEnd := true, false
	for _, l := range lines {
		if l.vStart != lines[0].vStart {
			uniformStart = false
		}
		if l.vEnd != lines[0].vEnd {
			varyingEnd = true
		}
	}
	switch {
	case singleSpaced:
		return LayoutMultiline
	case uniformStart && varyingEnd:
		return LayoutMaxlen
	case holdsEdge(lines, func(l crateLine) int { return l.vEnd },
		// A version that cannot physically fit the column — build
		// metadata makes some very long — is printed from the column's
		// left edge and overruns, in the tool's own justify output. It
		// never disqualifies: overflow is the layout working, not the
		// layout broken. The column's left edge is the block's smallest
		// version start; a long name can also push the start past it.
		func(l crateLine, modal int) bool {
			left := minVStart(lines)
			if l.nameEnd()+1 > left {
				left = l.nameEnd() + 1
			}
			return left+l.versionLen() > modal
		}):
		return LayoutJustify
	case holdsEdge(lines, func(l crateLine) int { return l.vStart },
		func(l crateLine, modal int) bool { return l.nameEnd()+1 > modal }):
		return LayoutMaxlen
	}
	return LayoutRagged
}

func (l crateLine) nameEnd() int    { return l.vStart - l.sep1 }
func (l crateLine) versionLen() int { return l.vEnd - l.vStart }

// minVStart is the block's version column's left edge: the smallest
// column any version starts in.
func minVStart(lines []crateLine) int {
	m := lines[0].vStart
	for _, l := range lines[1:] {
		if l.vStart < m {
			m = l.vStart
		}
	}
	return m
}

// holdsEdge reports whether one column value is the block's rule: a
// majority of lines share the modal value, and every line off it is a
// legitimate overflow — a field the rule could not have fit there.
func holdsEdge(lines []crateLine, edge func(crateLine) int, overflows func(l crateLine, modal int) bool) bool {
	counts := map[int]int{}
	for _, l := range lines {
		counts[edge(l)]++
	}
	modal, n := 0, 0
	for v, c := range counts {
		if c > n {
			modal, n = v, c
		}
	}
	if n*2 <= len(lines) {
		return false
	}
	for _, l := range lines {
		if edge(l) != modal && !overflows(l, modal) {
			return false
		}
	}
	return true
}

// measure reads one crate line's field columns: name, version,
// checksum, separated by runs of spaces.
func measure(line string) (crateLine, bool) {
	type field struct{ start, end int }
	var fields []field
	inField := false
	for i, r := range line {
		switch {
		case r != ' ' && !inField:
			fields = append(fields, field{start: i})
			inField = true
		case r == ' ' && inField:
			fields[len(fields)-1].end = i
			inField = false
		}
	}
	if inField {
		fields[len(fields)-1].end = len(line)
	}
	if len(fields) != 3 {
		return crateLine{}, false
	}
	return crateLine{
		vStart: fields[1].start, vEnd: fields[1].end, sStart: fields[2].start,
		sep1: fields[1].start - fields[0].end,
		sep2: fields[2].start - fields[1].end,
	}, true
}
