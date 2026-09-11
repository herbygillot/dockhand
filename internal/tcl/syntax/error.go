package syntax

import (
	"fmt"

	"github.com/herbygillot/dockhand/v2/internal/text"
)

type ErrorType int

const (
	UntermBrace ErrorType = iota

	UntermQuote

	UntermVarName

	UntermArrayIndex

	UntermCmdSub

	ExtraAfterCloseBrace

	ExtraAfterCloseQuote

	ListUntermBrace

	ListUntermQuote

	ListElementNotSpaced

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

type Error struct {
	Type ErrorType
	Span text.Span
}

func (e Error) Error() string {
	return fmt.Sprintf("%s at offset %d", e.Type, e.Span.Start)
}

func (e Error) Describe(src []byte) string {
	line, col := text.Position(src, e.Span.Start)
	return fmt.Sprintf("%d:%d: %s", line, col, e.Type)
}
