package cargo2port

import "strings"

// Layout is a cargo.crates column layout, named in the generator's own
// vocabulary (cargo2port's --align values) plus ragged for a block in
// none of them.
type Layout string

const (
	// LayoutJustify right-aligns versions within a fixed-width field —
	// the tree's convention. The field slides right past a long name,
	// and a version too long for it starts at the field's left edge
	// and overruns; both are the layout working, not broken.
	LayoutJustify Layout = "justify"
	// LayoutMaxlen pads names and leaves versions left-aligned.
	LayoutMaxlen Layout = "maxlen"
	// LayoutMultiline separates fields with single spaces.
	LayoutMultiline Layout = "multiline"
	// LayoutRagged is a block whose geometry could not be proven:
	// regeneration falls back to the tool's own output verbatim.
	LayoutRagged Layout = "ragged"
)

// alignFlag is the generator flag a layout maps back to.
func (l Layout) alignFlag() string {
	if l == LayoutRagged {
		l = LayoutJustify
	}
	return "--align=" + string(l)
}

// Geometry is a block's measured column rule, complete enough to
// reproduce it: every crate line is
//
//	<indent>name<gap>version<shaSep>checksum
//
// where the version sits in a field starting at max(ColLeft,
// nameEnd+MinSep) — right-aligned within VWidth columns when VWidth is
// positive (overflowing from the field's left edge when too long),
// left-aligned otherwise. A Geometry is only handed out after Assess
// has proven it reproduces the existing block byte for byte, so
// holding one is holding a proof.
type Geometry struct {
	Layout  Layout
	Option  string // the block's own first word: cargo.crates or -append
	Indent  int
	ColLeft int
	VWidth  int
	// FixedVEnd, when positive, is the other justify rule: versions
	// right-align to this end column directly (cargo2port's own
	// output), rather than within a sliding VWidth-wide field (the
	// script-written blocks). A version that cannot reach it starts at
	// the field floor and overruns.
	FixedVEnd int
	MinSep    int
	// ShaSep is the minimum gap before the checksum; ShaLeft, when
	// positive, is a fixed column the checksum left-aligns to, the gap
	// stretching to reach it.
	ShaSep  int
	ShaLeft int
}

// Assess measures an existing block's geometry and proves the
// measurement: candidate rules are derived from the block's modal
// columns, and the one that re-renders the block's own triples byte
// for byte wins. No candidate proving out means the block was written
// by a rule this package does not know — a hand script's, an edit's —
// and ok is false: regeneration then keeps the tool's verbatim output,
// the best effort there is.
func Assess(block string) (Geometry, bool) {
	option, crates, lines, ok := parseBlock(block)
	if !ok || len(lines) == 0 {
		return Geometry{Layout: LayoutRagged}, false
	}
	indent := modalOf(lines, func(l crateLine) int { return l.indent })
	shaSep := modalOf(lines, func(l crateLine) int { return l.sep2 })
	modalVEnd := modalOf(lines, func(l crateLine) int { return l.vEnd })
	modalVStart := modalOf(lines, func(l crateLine) int { return l.vStart })
	colLeft := lines[0].vStart
	for _, l := range lines {
		if l.vStart < colLeft {
			colLeft = l.vStart
		}
	}
	modalSha := modalOf(lines, func(l crateLine) int { return l.sStart })
	// Candidates only propose; the byte-for-byte round trip disposes,
	// so adding one can only widen what is reproducible.
	candidates := []Geometry{
		{Layout: LayoutJustify, Option: option, Indent: indent,
			FixedVEnd: modalVEnd, ColLeft: colLeft, MinSep: 2, ShaSep: shaSep},
		{Layout: LayoutJustify, Option: option, Indent: indent,
			FixedVEnd: modalVEnd, ColLeft: colLeft, MinSep: 2, ShaSep: 2, ShaLeft: modalSha},
		{Layout: LayoutJustify, Option: option, Indent: indent,
			ColLeft: colLeft, VWidth: modalVEnd - colLeft, MinSep: 2, ShaSep: shaSep},
		{Layout: LayoutJustify, Option: option, Indent: indent,
			ColLeft: colLeft, VWidth: modalVEnd - colLeft, MinSep: 2, ShaSep: 2, ShaLeft: modalSha},
		{Layout: LayoutMaxlen, Option: option, Indent: indent,
			ColLeft: modalVStart, MinSep: 2, ShaSep: 2, ShaLeft: modalSha},
		{Layout: LayoutMaxlen, Option: option, Indent: indent,
			ColLeft: modalVStart, MinSep: 2, ShaSep: shaSep},
		{Layout: LayoutMultiline, Option: option, Indent: indent,
			MinSep: 1, ShaSep: 1},
	}
	for _, g := range candidates {
		if string(Format(crates, g)) == block {
			return g, true
		}
	}
	return Geometry{Layout: LayoutRagged}, false
}

// Alignment is the family verdict alone — Assess's Layout, for
// reporting.
func Alignment(block string) Layout {
	g, _ := Assess(block)
	return g.Layout
}

// Format renders crates under a geometry, in the block shape a located
// span holds: continuation backslashes on every line but the last, no
// trailing newline.
func Format(crates []Crate, g Geometry) []byte {
	var b strings.Builder
	b.WriteString(g.Option)
	b.WriteString(" \\")
	for i, c := range crates {
		b.WriteByte('\n')
		b.WriteString(strings.Repeat(" ", g.Indent))
		b.WriteString(c.Name)
		nameEnd := g.Indent + len(c.Name)
		fieldStart := nameEnd + g.MinSep
		if g.ColLeft > fieldStart {
			fieldStart = g.ColLeft
		}
		vStart := fieldStart
		if g.FixedVEnd > 0 {
			if s := g.FixedVEnd - len(c.Version); s > fieldStart {
				vStart = s
			}
		} else if pad := g.VWidth - len(c.Version); pad > 0 {
			vStart += pad
		}
		b.WriteString(strings.Repeat(" ", vStart-nameEnd))
		b.WriteString(c.Version)
		vEnd := vStart + len(c.Version)
		shaStart := vEnd + g.ShaSep
		if g.ShaLeft > shaStart {
			shaStart = g.ShaLeft
		}
		b.WriteString(strings.Repeat(" ", shaStart-vEnd))
		b.WriteString(c.SHA256)
		if i != len(crates)-1 {
			b.WriteString(" \\")
		}
	}
	return []byte(b.String())
}

// Reformat re-renders a generated block under a proven geometry: the
// tool's output stays the source of every token, and only the
// whitespace between opaque words is re-laid. The geometry carries its
// source block's option word, which is what preserves -append.
// Anything unparseable comes back unchanged — the fallback is always
// the tool's own bytes.
func Reformat(block []byte, g Geometry) []byte {
	_, crates, _, ok := parseBlock(string(block))
	if !ok || len(crates) == 0 {
		return block
	}
	return Format(crates, g)
}

// crateLine is one measured entry: where its fields start and end, in
// byte columns.
type crateLine struct {
	indent, vStart, vEnd, sStart int
	sep1, sep2                   int
}

// parseBlock splits a block into its option word, its crate triples,
// and each line's measured columns. ok is false for anything that is
// not plainly lines of three space-separated fields — tabs included,
// because tab stops make byte and visual columns two different
// questions.
func parseBlock(block string) (option string, crates []Crate, lines []crateLine, ok bool) {
	first := true
	for raw := range strings.Lines(block) {
		line := strings.TrimSuffix(raw, "\n")
		line = strings.TrimSuffix(line, " \\")
		if first {
			first = false
			option = strings.TrimSpace(line)
			continue
		}
		if strings.Contains(line, "\t") {
			return "", nil, nil, false
		}
		m, c, mok := measure(line)
		if !mok {
			return "", nil, nil, false
		}
		crates = append(crates, c)
		lines = append(lines, m)
	}
	if option == "" || strings.ContainsAny(option, " \t\\") {
		return "", nil, nil, false
	}
	return option, crates, lines, true
}

// modalOf is the most common value of one measured column.
func modalOf(lines []crateLine, f func(crateLine) int) int {
	counts := map[int]int{}
	for _, l := range lines {
		counts[f(l)]++
	}
	best, n := 0, 0
	for v, c := range counts {
		if c > n {
			best, n = v, c
		}
	}
	return best
}

// measure reads one crate line's fields and their columns: name,
// version, checksum, separated by runs of spaces.
func measure(line string) (crateLine, Crate, bool) {
	type field struct{ start, end int }
	var fields []field
	var words []string
	inField, start := false, 0
	for i, r := range line {
		switch {
		case r != ' ' && !inField:
			start, inField = i, true
		case r == ' ' && inField:
			fields = append(fields, field{start, i})
			words = append(words, line[start:i])
			inField = false
		}
	}
	if inField {
		fields = append(fields, field{start, len(line)})
		words = append(words, line[start:])
	}
	if len(fields) != 3 {
		return crateLine{}, Crate{}, false
	}
	return crateLine{
			indent: fields[0].start,
			vStart: fields[1].start, vEnd: fields[1].end,
			sStart: fields[2].start,
			sep1:   fields[1].start - fields[0].end,
			sep2:   fields[2].start - fields[1].end,
		}, Crate{Name: words[0], Version: words[1], SHA256: words[2]},
		true
}
