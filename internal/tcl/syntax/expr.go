package syntax

import "github.com/herbygillot/dockhand/internal/text"

// Expr is a node of a Tcl expression, the language of an if or while
// condition and of expr's argument. The tree gives a reader the shape of a
// condition, which operand a comparison holds and which variables and
// commands it reads, without evaluating anything.
type Expr interface{ expr() }

// Number is a numeric literal.
type Number struct{ Span text.Span }

// Text is a quoted or braced operand. Its Word carries the segments, so a
// quoted string's substitutions are reachable and a braced one has none.
type Text struct {
	Span text.Span
	Word Word
}

// Variable is a variable read.
type Variable struct{ VarSub }

// Call is a command substitution.
type Call struct{ CmdSub }

// Bareword is one of Tcl's boolean words: true, false, yes, no, on, off.
type Bareword struct{ Span text.Span }

// Unary is ! - or + applied to an operand.
type Unary struct {
	Span text.Span
	Op   string
	X    Expr
}

// Binary is an operator between two operands.
type Binary struct {
	Span text.Span
	Op   string
	L, R Expr
}

// Group is a parenthesized expression.
type Group struct {
	Span text.Span
	X    Expr
}

func (Number) expr()   {}
func (Text) expr()     {}
func (Variable) expr() {}
func (Call) expr()     {}
func (Bareword) expr() {}
func (Unary) expr()    {}
func (Binary) expr()   {}
func (Group) expr()    {}

// ExprSpan is the source span of a node.
func ExprSpan(e Expr) text.Span {
	switch e := e.(type) {
	case Number:
		return e.Span
	case Text:
		return e.Span
	case Variable:
		return e.VarSub.Span
	case Call:
		return e.CmdSub.Span
	case Bareword:
		return e.Span
	case Unary:
		return e.Span
	case Binary:
		return e.Span
	case Group:
		return e.Span
	}
	panic("syntax: unknown expression kind")
}

// WalkExpr visits every node, parents before children, until visit returns
// false for a node, whose children are then skipped.
func WalkExpr(e Expr, visit func(Expr) bool) {
	if e == nil || !visit(e) {
		return
	}
	switch e := e.(type) {
	case Unary:
		WalkExpr(e.X, visit)
	case Binary:
		WalkExpr(e.L, visit)
		WalkExpr(e.R, visit)
	case Group:
		WalkExpr(e.X, visit)
	}
}

// ComparisonOps are the operators whose operands a reader may want to
// relate: the relational and equality operators, and in and ni.
var ComparisonOps = map[string]bool{"<": true, ">": true, "<=": true, ">=": true, "==": true, "!=": true, "eq": true, "ne": true, "in": true, "ni": true}

// ParseExpr parses the expression in the window. It reports a syntax error
// where Tcl would, and ExprUnsupported for the forms this parser does not
// model, the ternary, the bitwise and shift operators, exponentiation, and
// function calls, so a reader refuses them rather than misreading them.
func ParseExpr(src []byte, window text.Span) (Expr, []Error) {
	p := &exprParser{parser: parser{src: src, pos: window.Start, end: window.End}}
	e := p.or()
	p.skip()
	if p.pos < p.end && len(p.errs) == 0 {
		p.addError(span(p.pos, p.pos), ExprUnexpected)
	}
	if len(p.errs) != 0 {
		return nil, p.errs
	}
	return e, nil
}

type exprParser struct{ parser }

func (p *exprParser) skip() {
	for p.pos < p.end {
		c := p.src[p.pos]
		switch {
		case isSpace(c) || c == '\n':
			p.pos++
		case c == '\\' && p.pos+1 < p.end && p.src[p.pos+1] == '\n':
			p.pos += 2
		default:
			return
		}
	}
}

func isIdentChar(c byte) bool { return isVarNameChar(c) }

// operator consumes one of the given operators at the position, longest
// first, when it is there. A word operator must end the word.
func (p *exprParser) operator(candidates ...string) string {
	p.skip()
	for _, op := range candidates {
		if p.pos+len(op) > p.end || string(p.src[p.pos:p.pos+len(op)]) != op {
			continue
		}
		if isIdentChar(op[0]) && p.pos+len(op) < p.end && isIdentChar(p.src[p.pos+len(op)]) {
			continue
		}
		p.pos += len(op)
		return op
	}
	return ""
}

// unsupported refuses an operator Tcl accepts but this parser does not model.
func (p *exprParser) unsupported() bool {
	p.skip()
	for _, op := range []string{"**", "<<", ">>", "?", ":", "^", "~"} {
		if p.pos+len(op) <= p.end && string(p.src[p.pos:p.pos+len(op)]) == op {
			p.addError(span(p.pos, p.pos+len(op)), ExprUnsupported)
			return true
		}
	}
	// A single & or | is bitwise; the logical forms are consumed by their levels.
	if p.pos < p.end && (p.src[p.pos] == '&' || p.src[p.pos] == '|') && !(p.pos+1 < p.end && p.src[p.pos+1] == p.src[p.pos]) {
		p.addError(span(p.pos, p.pos+1), ExprUnsupported)
		return true
	}
	return false
}

func (p *exprParser) binary(next func() Expr, ops ...string) Expr {
	left := next()
	for len(p.errs) == 0 {
		if p.unsupported() {
			return left
		}
		op := p.operator(ops...)
		if op == "" {
			return left
		}
		right := next()
		if right == nil {
			return left
		}
		left = Binary{Span: span(ExprSpan(left).Start, ExprSpan(right).End), Op: op, L: left, R: right}
	}
	return left
}

func (p *exprParser) or() Expr  { return p.binary(p.and, "||") }
func (p *exprParser) and() Expr { return p.binary(p.equality, "&&") }
func (p *exprParser) equality() Expr {
	return p.binary(p.relational, "==", "!=", "eq", "ne", "in", "ni")
}
func (p *exprParser) relational() Expr { return p.binary(p.additive, "<=", ">=", "<", ">") }
func (p *exprParser) additive() Expr   { return p.binary(p.multiplicative, "+", "-") }
func (p *exprParser) multiplicative() Expr {
	return p.binary(p.unary, "*", "/", "%")
}

func (p *exprParser) unary() Expr {
	p.skip()
	if p.pos < p.end {
		switch c := p.src[p.pos]; c {
		case '!', '-', '+':
			start := p.pos
			p.pos++
			x := p.unary()
			if x == nil {
				return nil
			}
			return Unary{Span: span(start, ExprSpan(x).End), Op: string(c), X: x}
		}
	}
	return p.primary()
}

var barewords = map[string]bool{"true": true, "false": true, "yes": true, "no": true, "on": true, "off": true}

func (p *exprParser) primary() Expr {
	p.skip()
	if p.pos >= p.end {
		p.addError(span(p.pos, p.pos), ExprUnexpected)
		return nil
	}
	start := p.pos
	switch c := p.src[p.pos]; {
	case c == '(':
		p.pos++
		x := p.or()
		p.skip()
		if x == nil || p.pos >= p.end || p.src[p.pos] != ')' {
			if len(p.errs) == 0 {
				p.addError(span(start, p.pos), ExprUnexpected)
			}
			return nil
		}
		p.pos++
		return Group{Span: span(start, p.pos), X: x}
	case c == '$':
		segments := p.varOrLiteralDollar(nil)
		if v, ok := segments[len(segments)-1].(VarSub); ok && len(p.errs) == 0 {
			return Variable{v}
		}
		if len(p.errs) == 0 {
			p.addError(span(start, p.pos), ExprUnexpected)
		}
		return nil
	case c == '[':
		segment := p.cmdSub()
		if len(p.errs) != 0 {
			return nil
		}
		return Call{segment.(CmdSub)}
	case c == '"':
		segment := p.quotedWord()
		if len(p.errs) != 0 {
			return nil
		}
		return Text{Span: span(start, p.pos), Word: Word{Span: span(start, p.pos), Segments: []Segment{segment}}}
	case c == '{':
		segment := p.bracedWord()
		if len(p.errs) != 0 {
			return nil
		}
		return Text{Span: span(start, p.pos), Word: Word{Span: span(start, p.pos), Segments: []Segment{segment}}}
	case c >= '0' && c <= '9' || c == '.' && p.pos+1 < p.end && p.src[p.pos+1] >= '0' && p.src[p.pos+1] <= '9':
		for p.pos < p.end && (isIdentChar(p.src[p.pos]) || p.src[p.pos] == '.') {
			p.pos++
		}
		return Number{Span: span(start, p.pos)}
	case isIdentChar(c):
		for p.pos < p.end && isIdentChar(p.src[p.pos]) {
			p.pos++
		}
		word := string(p.src[start:p.pos])
		p.skip()
		if p.pos < p.end && p.src[p.pos] == '(' {
			p.addError(span(start, p.pos+1), ExprUnsupported)
			return nil
		}
		if barewords[word] {
			return Bareword{Span: span(start, start+len(word))}
		}
		p.addError(span(start, start+len(word)), ExprUnexpected)
		return nil
	}
	if !p.unsupported() {
		p.addError(span(start, start+1), ExprUnexpected)
	}
	return nil
}

// Variables lists every variable read in the word's segments, at any depth:
// a quoted word's substitutions, and the words of any command substitution
// it contains. A braced word reads nothing.
func (w Word) Variables(src []byte) []VarSub {
	var reads []VarSub
	var segments func([]Segment)
	segments = func(segs []Segment) {
		for _, segment := range segs {
			switch value := segment.(type) {
			case VarSub:
				reads = append(reads, value)
			case Quoted:
				segments(value.Segments)
			case CmdSub:
				for command := range value.Script.Commands(src, func(Command) bool { return true }) {
					for _, word := range command.Words {
						reads = append(reads, word.Variables(src)...)
					}
				}
			}
		}
	}
	segments(w.Segments)
	return reads
}

// ExprVariables lists every variable an expression reads, at any depth,
// including inside its quoted operands and its command substitutions.
func ExprVariables(src []byte, e Expr) []VarSub {
	var reads []VarSub
	WalkExpr(e, func(node Expr) bool {
		switch node := node.(type) {
		case Variable:
			reads = append(reads, node.VarSub)
		case Text:
			reads = append(reads, node.Word.Variables(src)...)
		case Call:
			reads = append(reads, Word{Segments: []Segment{node.CmdSub}}.Variables(src)...)
		}
		return true
	})
	return reads
}
