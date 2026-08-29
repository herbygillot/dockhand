package syntax

import (
	"fmt"

	"github.com/herbygillot/dockhand/internal/text"
)

// ErrorType identifies which Dodekalogue rule an Error violates. The set is
// closed: these are the only ways input can deviate from Tcl's lexical
// rules, because the rules reject nothing else.
type ErrorType int

const (
	// UntermBrace is a braced word with no matching close brace (rule [6]).
	UntermBrace ErrorType = iota
	// UntermQuote is a quoted word with no closing double quote (rule [4]).
	UntermQuote
	// UntermVarName is a ${name} form with no closing brace (rule [8]).
	UntermVarName
	// UntermArrayIndex is a $name(index) form with no close paren (rule [8]).
	UntermArrayIndex
	// UntermCmdSub is a command substitution with no closing bracket (rule [7]).
	UntermCmdSub
	// ExtraAfterCloseBrace is a closed braced word followed by characters
	// instead of a word boundary (rule [6]).
	ExtraAfterCloseBrace
	// ExtraAfterCloseQuote is a closed quoted word followed by characters
	// instead of a word boundary (rule [4]).
	ExtraAfterCloseQuote
	// ListUntermBrace is an unterminated braced element in a Tcl list.
	ListUntermBrace
	// ListUntermQuote is an unterminated quoted element in a Tcl list.
	ListUntermQuote
	// ListElementNotSpaced is a list element followed by characters instead
	// of whitespace.
	ListElementNotSpaced
	// DictMissingValue is a dict whose list form has odd length: a key with
	// no value.
	DictMissingValue
)

func (t ErrorType) String() string {
	switch t {
	case UntermBrace:
		return "unterminated brace"
	case UntermQuote:
		return "unterminated quote"
	case UntermVarName:
		return "unterminated ${name}"
	case UntermArrayIndex:
		return "missing close-paren in array index"
	case UntermCmdSub:
		return "unterminated command substitution"
	case ExtraAfterCloseBrace:
		return "extra characters after close-brace"
	case ExtraAfterCloseQuote:
		return "extra characters after close-quote"
	case ListUntermBrace:
		return "unterminated brace in list"
	case ListUntermQuote:
		return "unterminated quote in list"
	case ListElementNotSpaced:
		return "list element not followed by space"
	case DictMissingValue:
		return "missing value to go with key"
	}
	return "unknown error"
}

// Error records a place where the source deviates from Tcl syntax — every
// type corresponds to something tclsh itself rejects. Span covers the
// offending construct: for unterminated constructs it runs from the opener
// to the end of the parse window; for extra-characters errors it is a
// zero-length position marker. The parser continues past errors and still
// returns a tiling tree; callers that require a clean parse treat any error
// as grounds to decline.
type Error struct {
	Type ErrorType
	Span text.Span
}

// Error implements the error interface. The message carries the byte
// offset; Describe renders line and column when the source is at hand.
func (e Error) Error() string {
	return fmt.Sprintf("%s at offset %d", e.Type, e.Span.Start)
}

// Describe renders the error against its source as "line:col: message".
func (e Error) Describe(src []byte) string {
	line, col := text.Position(src, e.Span.Start)
	return fmt.Sprintf("%d:%d: %s", line, col, e.Type)
}
