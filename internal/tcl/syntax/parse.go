package syntax

import "github.com/herbygillot/dockhand/internal/text"

func Parse(src []byte) (*Script, []Error) {
	return ParseScript(src, span(0, len(src)))
}

func ParseScript(src []byte, window text.Span) (*Script, []Error) {
	p := &parser{src: src, pos: window.Start, end: window.End}
	s := p.script()
	s.Span = window
	return s, p.errs
}

func (b Braced) scriptLens(src []byte) (*Script, []Error) {
	return ParseScript(src, b.Body)
}

type parser struct {
	src  []byte
	pos  int
	end  int
	brk  int
	errs []Error
}

func (p *parser) addError(sp text.Span, typ ErrorType) {
	p.errs = append(p.errs, Error{Type: typ, Span: sp})
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\v' || c == '\f'
}

func isVarNameChar(c byte) bool {
	return c == '_' ||
		('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9')
}

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

	if p.pos+3 < p.end &&
		p.src[p.pos] == '{' && p.src[p.pos+1] == '*' && p.src[p.pos+2] == '}' {

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

func (p *parser) bracedWord() Segment {
	start := p.pos
	p.pos++
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

func (p *parser) quotedWord() Segment {
	start := p.pos
	p.pos++
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

func (p *parser) varOrLiteralDollar(segs []Segment) []Segment {
	start := p.pos
	p.pos++

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

		return append(segs, Literal{span(start, p.pos)})
	}
	return append(segs, VarSub{Span: span(start, p.pos), Name: name})
}

func (p *parser) cmdSub() Segment {
	start := p.pos
	p.pos++
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
