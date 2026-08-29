package syntax

import "github.com/herbygillot/dockhand/internal/text"

// Every node locates itself with a text.Span, so spans produced here flow
// into text.Apply and the rest of the edit machinery without conversion.

// span is the package-local constructor, keeping parser code terse.
func span(start, end int) text.Span { return text.Span{Start: start, End: end} }

// Script is a parsed sequence of commands and comments. For a top-level
// parse its span covers the whole window; for a command substitution it
// covers the bytes between the brackets.
type Script struct {
	Span  text.Span
	Items []Item
}

// Item is one top-level element of a Script.
//
//sumtype:decl
type Item interface{ item() }

// Comment is a #-comment occupying command position. Its span runs from the
// hash to the end of the comment, excluding the terminating newline. A
// backslash-newline continues a comment onto the next line, so a span may
// cover several lines.
type Comment struct {
	Span text.Span
}

// Command is one command: a non-empty sequence of words.
type Command struct {
	Span  text.Span
	Words []Word
}

func (Comment) item() {}
func (Command) item() {}

// Word is one word of a command. A word whose first character is an open
// brace or double quote is represented as a single Braced or Quoted
// segment; otherwise it is a sequence of Literal, VarSub, and CmdSub
// segments. Expand records a leading {*} expansion prefix (rule [5]), which
// is included in the word's span but not in any segment.
type Word struct {
	Span     text.Span
	Expand   bool
	Segments []Segment
}

// Segment is a run of bytes within a word, classified by the substitution
// rule that governs it.
//
//sumtype:decl
type Segment interface{ segment() }

// Literal is a run of ordinary characters, including backslash escapes.
// Escapes are not decoded here; a Literal's bytes are exactly its source
// bytes.
type Literal struct {
	Span text.Span
}

// VarSub is a variable substitution: $name, ${name}, or $name(index).
// Name is the span of the variable name proper (empty for the $(index)
// array form). When HasIndex is set, Index spans the bytes between the
// parentheses; its internal segmentation is not yet parsed.
type VarSub struct {
	Span     text.Span
	Name     text.Span
	Index    text.Span
	HasIndex bool
}

// CmdSub is a command substitution: a bracketed script inside a word.
// Script covers the bytes between the brackets.
type CmdSub struct {
	Span   text.Span
	Script *Script
}

// Braced is a whole word quoted by braces. Span includes the braces; Body
// is the raw bytes between them. The body's meaning belongs to the command
// consuming it — reinterpret it with ScriptLens or ListLens.
type Braced struct {
	Span text.Span
	Body text.Span
}

// Quoted is a whole word quoted by double quotes. Span includes the quotes;
// Segments cover the bytes between them.
type Quoted struct {
	Span     text.Span
	Segments []Segment
}

func (Literal) segment() {}
func (VarSub) segment()  {}
func (CmdSub) segment()  {}
func (Braced) segment()  {}
func (Quoted) segment()  {}

// Name returns the command's name — the text of its first word — when that
// word is a single literal segment, which is the common case recognizers
// care about. The second result is false for computed or quoted names.
func (c Command) Name(src []byte) (string, bool) {
	if len(c.Words) == 0 {
		return "", false
	}
	return c.Words[0].Literal(src)
}

// Literal returns the word's text when the word is a single literal
// segment — a word whose text is its value, verbatim. The second result
// is false for quoted, braced, substituted, computed, and {*}-expanded
// words.
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

// BracedScript returns the word's brace body read as a script, when the
// word is a single braced segment whose body parses cleanly. The second
// result is false otherwise — including for {*}-expanded words, whose
// braced body is a list being spliced, not a script. Whether a braced
// word IS a script belongs to the command consuming it; this only
// answers how to read it as one.
func (w Word) BracedScript(src []byte) (*Script, bool) {
	if w.Expand || len(w.Segments) != 1 {
		return nil, false
	}
	braced, ok := w.Segments[0].(Braced)
	if !ok {
		return nil, false
	}
	body, errs := braced.ScriptLens(src)
	if len(errs) != 0 {
		return nil, false
	}
	return body, true
}
