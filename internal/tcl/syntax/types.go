package syntax

import "github.com/herbygillot/dockhand/internal/textedit"

func span(start, end int) textedit.Span { return textedit.Span{Start: start, End: end} }

type Script struct {
	Span  textedit.Span
	Items []Item
}

type Item interface{ item() }

type Comment struct {
	Span textedit.Span
}

type Command struct {
	Span  textedit.Span
	Words []Word
}

func (Comment) item() {}
func (Command) item() {}

type Word struct {
	Span     textedit.Span
	Expand   bool
	Segments []Segment
}

type Segment interface{ segment() }

type Literal struct {
	Span textedit.Span
}

type VarSub struct {
	Span     textedit.Span
	Name     textedit.Span
	Index    textedit.Span
	HasIndex bool
}

type CmdSub struct {
	Span   textedit.Span
	Script *Script
}

type Braced struct {
	Span textedit.Span
	Body textedit.Span
}

type Quoted struct {
	Span     textedit.Span
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
