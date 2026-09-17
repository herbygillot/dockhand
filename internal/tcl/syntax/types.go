package syntax

import "github.com/herbygillot/dockhand/internal/text"

func span(start, end int) text.Span { return text.Span{Start: start, End: end} }

type Script struct {
	Span  text.Span
	Items []Item
}

type Item interface{ item() }

type Comment struct {
	Span text.Span
}

type Command struct {
	Span  text.Span
	Words []Word
}

func (Comment) item() {}
func (Command) item() {}

type Word struct {
	Span     text.Span
	Expand   bool
	Segments []Segment
}

type Segment interface{ segment() }

type Literal struct {
	Span text.Span
}

type VarSub struct {
	Span     text.Span
	Name     text.Span
	Index    text.Span
	HasIndex bool
}

type CmdSub struct {
	Span   text.Span
	Script *Script
}

type Braced struct {
	Span text.Span
	Body text.Span
}

type Quoted struct {
	Span     text.Span
	Segments []Segment
}

func (Literal) segment() {}
func (VarSub) segment()  {}
func (CmdSub) segment()  {}
func (Braced) segment()  {}
func (Quoted) segment()  {}

func (c Command) Name(src []byte) (string, bool) {
	if len(c.Words) == 0 {
		return "", false
	}
	return c.Words[0].Literal(src)
}

func (w Word) Literal(src []byte) (string, bool) {
	if w.Expand || len(w.Segments) != 1 {
		return "", false
	}
	lit, ok := w.Segments[0].(Literal)
	if !ok {
		return "", false
	}
	return lit.Span.Text(src), true
}

func (w Word) BracedScript(src []byte) (*Script, bool) {
	if w.Expand || len(w.Segments) != 1 {
		return nil, false
	}
	braced, ok := w.Segments[0].(Braced)
	if !ok {
		return nil, false
	}
	body, errs := braced.scriptLens(src)
	if len(errs) != 0 {
		return nil, false
	}
	return body, true
}
