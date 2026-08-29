package syntax

import "github.com/herbygillot/dockhand/internal/text"

// ListLens reinterprets the brace body as a Tcl list, returning the span of
// each element, delimiters included. List syntax is close to command syntax
// but not the same: no substitutions, no comments, and newlines are plain
// whitespace. Elements are bare runs, brace-quoted (nesting, with
// backslash-quoted braces not counted), or double-quoted (with backslash
// escapes).
func (b Braced) ListLens(src []byte) ([]text.Span, []Error) {
	return SplitList(src, b.Body)
}

// SplitList splits the given window of src as a Tcl list. Spans are
// absolute offsets into src.
func SplitList(src []byte, window text.Span) ([]text.Span, []Error) {
	var elems []text.Span
	var errs []Error
	pos, end := window.Start, window.End

	isListSpace := func(c byte) bool {
		return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f'
	}

	for {
		// Backslash-newline is whitespace here too: rule [9]'s prepass
		// applies in list context, so it separates elements rather than
		// forming one.
		for pos < end {
			if isListSpace(src[pos]) {
				pos++
			} else if src[pos] == '\\' && pos+1 < end && src[pos+1] == '\n' {
				pos += 2
			} else {
				break
			}
		}
		if pos >= end {
			return elems, errs
		}
		start := pos
		switch src[pos] {
		case '{':
			depth := 1
			pos++
			for pos < end && depth > 0 {
				c := src[pos]
				if c == '\\' && pos+1 < end {
					pos += 2
					continue
				}
				switch c {
				case '{':
					depth++
				case '}':
					depth--
				}
				pos++
			}
			if depth > 0 {
				errs = append(errs, Error{ListUntermBrace, span(start, end)})
				pos = end
			}
		case '"':
			pos++
			closed := false
			for pos < end {
				c := src[pos]
				if c == '\\' && pos+1 < end {
					pos += 2
					continue
				}
				pos++
				if c == '"' {
					closed = true
					break
				}
			}
			if !closed {
				errs = append(errs, Error{ListUntermQuote, span(start, end)})
			}
		default:
			for pos < end && !isListSpace(src[pos]) {
				if src[pos] == '\\' && pos+1 < end {
					if src[pos+1] == '\n' {
						// Backslash-newline ends the element, as a space would.
						break
					}
					pos += 2
					continue
				}
				pos++
			}
		}
		if pos < end && !isListSpace(src[pos]) {
			errs = append(errs, Error{ListElementNotSpaced, span(pos, pos)})
		}
		elems = append(elems, span(start, pos))
	}
}

// ListValue decodes the value a single list element denotes: a braced
// element yields its contents literally, a quoted element its contents with
// backslash escapes reduced, and a bare element its bytes with escapes
// reduced. It covers the escape profile Tcl's own list formatter emits —
// single-character backslash escapes — which is what evaluator replies
// contain; it is not a general rule [9] decoder (no \n, \xhh, or \uhhhh
// forms).
func ListValue(raw string) string {
	if len(raw) >= 2 && raw[0] == '{' && raw[len(raw)-1] == '}' {
		return raw[1 : len(raw)-1]
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		raw = raw[1 : len(raw)-1]
	}
	if !stringsContainsByte(raw, '\\') {
		return raw
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+1 < len(raw) {
			i++
		}
		out = append(out, raw[i])
	}
	return string(out)
}

func stringsContainsByte(s string, b byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return true
		}
	}
	return false
}

// ListValues decodes a Tcl list's string form into its element values.
func ListValues(s string) ([]string, []Error) {
	src := []byte(s)
	elems, errs := SplitList(src, text.Span{Start: 0, End: len(src)})
	if len(errs) != 0 {
		return nil, errs
	}
	out := make([]string, len(elems))
	for i, e := range elems {
		out[i] = ListValue(e.Text(src))
	}
	return out, nil
}

// DictValues decodes a Tcl dict's string form — a list of alternating keys
// and values — into a map. An odd-length list is the dict-shape violation
// tclsh reports as "missing value to go with key".
func DictValues(s string) (map[string]string, []Error) {
	vals, errs := ListValues(s)
	if len(errs) != 0 {
		return nil, errs
	}
	if len(vals)%2 != 0 {
		return nil, []Error{{Type: DictMissingValue, Span: text.Span{Start: 0, End: len(s)}}}
	}
	out := make(map[string]string, len(vals)/2)
	for i := 0; i < len(vals); i += 2 {
		out[vals[i]] = vals[i+1]
	}
	return out, nil
}
