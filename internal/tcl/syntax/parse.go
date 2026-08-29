package syntax

import "github.com/herbygillot/dockhand/internal/text"

// The parser implements rules [1], [3]–[10] of the Dodekalogue
// (docs/tcl-dodekalogue.md). Rules [2], [11], and [12] are evaluation
// semantics and produce no structure.
//
// Three rules do the heavy lifting and are worth naming here because they
// are the ones pattern-matching approaches get wrong:
//
//   - Braces and quotes are special only as the FIRST character of a word
//     (rules [4] and [6]); mid-word they are ordinary characters.
//   - A hash starts a comment only at command position (rule [10]).
//   - Backslash-newline is substituted everywhere, even inside braces
//     (rule [9]): it separates words in bare context, stays inside quoted
//     and braced content, and never counts toward brace depth.

// Parse parses an entire source buffer as a Tcl script.
func Parse(src []byte) (*Script, []Error) {
	return ParseScript(src, span(0, len(src)))
}

// ParseScript parses the given window of src as a Tcl script. Spans in the
// result are absolute offsets into src, which is what lets lenses re-parse
// a brace body in place: this is the entry point ScriptLens uses.
func ParseScript(src []byte, window text.Span) (*Script, []Error) {
	p := &parser{src: src, pos: window.Start, end: window.End}
	s := p.script()
	s.Span = window
	return s, p.errs
}

// ScriptLens reinterprets the brace body as a script, re-parsing it in
// place. Spans in the result are absolute offsets into src.
func (b Braced) ScriptLens(src []byte) (*Script, []Error) {
	return ParseScript(src, b.Body)
}

type parser struct {
	src  []byte
	pos  int
	end  int
	brk  int // >0 while inside [command substitution]: ']' terminates
	errs []Error
}

func (p *parser) addError(sp text.Span, typ ErrorType) {
	p.errs = append(p.errs, Error{Type: typ, Span: sp})
}

// isSpace reports intra-command whitespace: word separators that are not
// command separators.
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\v' || c == '\f'
}

func isVarNameChar(c byte) bool {
	return c == '_' ||
		('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9')
}

// script parses commands and comments until the window ends or, when the
// parser is inside brackets, until an unconsumed ']'.
func (p *parser) script() *Script {
	s := &Script{}
	for {
		p.skipCommandSeparators()
		if p.pos >= p.end {
			break
		}
		if p.brk > 0 && p.src[p.pos] == ']' {
			break
		}
		if p.src[p.pos] == '#' {
			s.Items = append(s.Items, p.comment())
		} else {
			s.Items = append(s.Items, p.command())
		}
	}
	return s
}

// skipCommandSeparators consumes everything legal between commands:
// whitespace, newlines, semicolons, and backslash-newline sequences.
func (p *parser) skipCommandSeparators() {
	for p.pos < p.end {
		c := p.src[p.pos]
		switch {
		case isSpace(c) || c == '\n' || c == ';':
			p.pos++
		case c == '\\' && p.pos+1 < p.end && p.src[p.pos+1] == '\n':
			p.pos += 2
		default:
			return
		}
	}
}

// skipWordSeparators consumes whitespace between words of one command:
// spaces, tabs, and backslash-newline (which rule [9] turns into a space).
func (p *parser) skipWordSeparators() {
	for p.pos < p.end {
		c := p.src[p.pos]
		switch {
		case isSpace(c):
			p.pos++
		case c == '\\' && p.pos+1 < p.end && p.src[p.pos+1] == '\n':
			p.pos += 2
		default:
			return
		}
	}
}

// atCommandEnd reports whether the parser sits on a command terminator.
func (p *parser) atCommandEnd() bool {
	if p.pos >= p.end {
		return true
	}
	c := p.src[p.pos]
	if c == '\n' || c == ';' {
		return true
	}
	return p.brk > 0 && c == ']'
}

// atWordBoundary reports whether the current position may legally follow a
// close brace or close quote.
func (p *parser) atWordBoundary() bool {
	if p.atCommandEnd() {
		return true
	}
	c := p.src[p.pos]
	if isSpace(c) {
		return true
	}
	return c == '\\' && p.pos+1 < p.end && p.src[p.pos+1] == '\n'
}

// comment consumes a #-comment. The span excludes the terminating newline.
// A backslash before the newline continues the comment (rule [9]'s prepass
// applies inside comments too).
func (p *parser) comment() Comment {
	start := p.pos
	for p.pos < p.end {
		c := p.src[p.pos]
		if c == '\\' && p.pos+1 < p.end {
			p.pos += 2
			continue
		}
		if c == '\n' {
			break
		}
		p.pos++
	}
	return Comment{Span: span(start, p.pos)}
}

func (p *parser) command() Command {
	var words []Word
	for {
		p.skipWordSeparators()
		if p.atCommandEnd() {
			break
		}
		words = append(words, p.word())
	}
	return Command{
		Span:  span(words[0].Span.Start, words[len(words)-1].Span.End),
		Words: words,
	}
}

func (p *parser) word() Word {
	start := p.pos
	expand := false
	// Rule [5]: a leading {*} followed by a non-whitespace character is the
	// expansion prefix, and the rest of the word is parsed as any other word.
	if p.pos+3 < p.end &&
		p.src[p.pos] == '{' && p.src[p.pos+1] == '*' && p.src[p.pos+2] == '}' {
		//nolint:staticcheck // QF1001: the negation mirrors the rule's phrasing — "unless inside brackets and c closes them".
		if c := p.src[p.pos+3]; !isSpace(c) && c != '\n' && c != ';' && !(p.brk > 0 && c == ']') {
			expand = true
			p.pos += 3
		}
	}

	var segs []Segment
	switch {
	case p.pos < p.end && p.src[p.pos] == '{':
		segs = append(segs, p.bracedWord())
	case p.pos < p.end && p.src[p.pos] == '"':
		segs = append(segs, p.quotedWord())
	default:
		segs = p.bareSegments(nil)
	}

	// Rule [4]/[6]: a closed brace or quote must be followed by a word
	// boundary. Tcl errors here; we record the issue and absorb the
	// trailing characters into the word so the tree still tiles the input.
	if !p.atWordBoundary() {
		if _, ok := segs[len(segs)-1].(Braced); ok {
			p.addError(span(p.pos, p.pos), ExtraAfterCloseBrace)
			segs = p.bareSegments(segs)
		} else if _, ok := segs[len(segs)-1].(Quoted); ok {
			p.addError(span(p.pos, p.pos), ExtraAfterCloseQuote)
			segs = p.bareSegments(segs)
		}
	}

	return Word{Span: span(start, p.pos), Expand: expand, Segments: segs}
}

// bracedWord parses a whole word quoted by braces (rule [6]). Braces nest;
// a backslash quotes the next character, so backslash-quoted braces do not
// count toward depth, and backslash-newline hides inside the body.
func (p *parser) bracedWord() Segment {
	start := p.pos
	p.pos++ // past '{'
	depth := 1
	bodyStart := p.pos
	for p.pos < p.end && depth > 0 {
		c := p.src[p.pos]
		if c == '\\' && p.pos+1 < p.end {
			p.pos += 2
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
		}
		p.pos++
	}
	if depth > 0 {
		p.addError(span(start, p.end), UntermBrace)
		return Braced{Span: span(start, p.end), Body: span(bodyStart, p.end)}
	}
	return Braced{Span: span(start, p.pos), Body: span(bodyStart, p.pos-1)}
}

// quotedWord parses a whole word quoted by double quotes (rule [4]).
// Variable and command substitution stay active inside; newlines,
// semicolons, and close brackets become ordinary characters.
func (p *parser) quotedWord() Segment {
	start := p.pos
	p.pos++ // past '"'
	var segs []Segment
	litStart := p.pos
	flush := func() {
		if p.pos > litStart {
			segs = append(segs, Literal{span(litStart, p.pos)})
		}
	}
	for p.pos < p.end {
		switch c := p.src[p.pos]; c {
		case '\\':
			if p.pos+1 < p.end {
				p.pos += 2
			} else {
				p.pos++
			}
		case '"':
			flush()
			p.pos++
			return Quoted{Span: span(start, p.pos), Segments: segs}
		case '$':
			flush()
			segs = p.varOrLiteralDollar(segs)
			litStart = p.pos
		case '[':
			flush()
			segs = append(segs, p.cmdSub())
			litStart = p.pos
		default:
			p.pos++
		}
	}
	flush()
	p.addError(span(start, p.end), UntermQuote)
	return Quoted{Span: span(start, p.end), Segments: segs}
}

// bareSegments parses word content outside braces and quotes, appending to
// segs, until a word separator or command terminator. Mid-word braces and
// quotes are ordinary characters here.
func (p *parser) bareSegments(segs []Segment) []Segment {
	litStart := p.pos
	flush := func() {
		if p.pos > litStart {
			segs = append(segs, Literal{span(litStart, p.pos)})
		}
	}
	for p.pos < p.end {
		c := p.src[p.pos]
		if isSpace(c) || c == '\n' || c == ';' {
			break
		}
		if p.brk > 0 && c == ']' {
			break
		}
		switch c {
		case '\\':
			if p.pos+1 < p.end && p.src[p.pos+1] == '\n' {
				// Rule [9]: backslash-newline becomes a space, ending the word.
				flush()
				return segs
			}
			if p.pos+1 < p.end {
				p.pos += 2
			} else {
				p.pos++
			}
		case '$':
			flush()
			segs = p.varOrLiteralDollar(segs)
			litStart = p.pos
		case '[':
			flush()
			segs = append(segs, p.cmdSub())
			litStart = p.pos
		default:
			p.pos++
		}
	}
	flush()
	return segs
}

// varOrLiteralDollar parses a variable substitution at '$', or, when no
// variable form follows (rule [8]), leaves the dollar as a literal.
func (p *parser) varOrLiteralDollar(segs []Segment) []Segment {
	start := p.pos
	p.pos++ // past '$'

	// ${name}: any characters except close brace, no substitutions inside.
	if p.pos < p.end && p.src[p.pos] == '{' {
		nameStart := p.pos + 1
		q := nameStart
		for q < p.end && p.src[q] != '}' {
			q++
		}
		if q >= p.end {
			p.addError(span(start, p.end), UntermVarName)
			p.pos = p.end
			return append(segs, VarSub{Span: span(start, p.end), Name: span(nameStart, p.end)})
		}
		p.pos = q + 1
		return append(segs, VarSub{Span: span(start, p.pos), Name: span(nameStart, q)})
	}

	// $name: letters, digits, underscores, and :: namespace separators.
	nameStart := p.pos
	for p.pos < p.end {
		c := p.src[p.pos]
		if isVarNameChar(c) {
			p.pos++
			continue
		}
		if c == ':' && p.pos+1 < p.end && p.src[p.pos+1] == ':' {
			for p.pos < p.end && p.src[p.pos] == ':' {
				p.pos++
			}
			continue
		}
		break
	}
	name := span(nameStart, p.pos)

	// $name(index) — including the empty-name array form $(index). The
	// index undergoes substitution in Tcl, so it may contain whitespace and
	// nested brackets; it ends at the first close paren outside brackets.
	if p.pos < p.end && p.src[p.pos] == '(' {
		idxStart := p.pos + 1
		q := idxStart
		bdepth := 0
		for q < p.end {
			c := p.src[q]
			if c == '\\' && q+1 < p.end {
				q += 2
				continue
			}
			if c == '[' {
				bdepth++
			} else if c == ']' && bdepth > 0 {
				bdepth--
			} else if c == ')' && bdepth == 0 {
				break
			}
			q++
		}
		if q >= p.end {
			p.addError(span(start, p.end), UntermArrayIndex)
			p.pos = p.end
			return append(segs, VarSub{Span: span(start, p.end), Name: name,
				Index: span(idxStart, p.end), HasIndex: true})
		}
		p.pos = q + 1
		return append(segs, VarSub{Span: span(start, p.pos), Name: name,
			Index: span(idxStart, q), HasIndex: true})
	}

	if name.Len() == 0 {
		// Bare '$' with no variable form: an ordinary character.
		return append(segs, Literal{span(start, p.pos)})
	}
	return append(segs, VarSub{Span: span(start, p.pos), Name: name})
}

// cmdSub parses a command substitution at '[' (rule [7]): a recursively
// parsed script terminated by the matching close bracket.
func (p *parser) cmdSub() Segment {
	start := p.pos
	p.pos++ // past '['
	bodyStart := p.pos
	p.brk++
	inner := p.script()
	p.brk--
	if p.pos < p.end && p.src[p.pos] == ']' {
		inner.Span = span(bodyStart, p.pos)
		p.pos++
		return CmdSub{Span: span(start, p.pos), Script: inner}
	}
	p.addError(span(start, p.end), UntermCmdSub)
	inner.Span = span(bodyStart, p.end)
	return CmdSub{Span: span(start, p.end), Script: inner}
}
