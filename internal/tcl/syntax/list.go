package syntax

import (
	"unicode/utf8"

	"github.com/herbygillot/dockhand/v2/internal/text"
)

func (b Braced) ListLens(src []byte) ([]text.Span, []Error) {
	return SplitList(src, b.Body)
}

func SplitList(src []byte, window text.Span) ([]text.Span, []Error) {
	var elems []text.Span
	var errs []Error
	pos, end := window.Start, window.End

	isListSpace := func(c byte) bool {
		return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f'
	}

	for {

		for pos < end {
			if isListSpace(src[pos]) {
				pos++
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
						pos += 2
						for pos < end && (src[pos] == ' ' || src[pos] == '\t') {
							pos++
						}
						continue
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
		if raw[i] != '\\' || i+1 == len(raw) {
			out = append(out, raw[i])
			continue
		}
		i++
		switch c := raw[i]; c {
		case 'a':
			out = append(out, '\a')
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'v':
			out = append(out, '\v')
		case '\n':
			for i+1 < len(raw) && (raw[i+1] == ' ' || raw[i+1] == '\t') {
				i++
			}
			out = append(out, ' ')
		case 'x', 'u', 'U':
			limit := 2
			if c == 'u' {
				limit = 4
			} else if c == 'U' {
				limit = 8
			}
			value, count := rune(0), 0
			for count < limit && i+1 < len(raw) {
				digit := hexDigit(raw[i+1])
				if digit < 0 || value*16+rune(digit) > utf8.MaxRune {
					break
				}
				value = value*16 + rune(digit)
				i++
				count++
			}
			if count == 0 {
				out = append(out, c)
			} else {
				out = utf8.AppendRune(out, value)
			}
		default:
			if c < '0' || c > '7' {
				out = append(out, c)
				continue
			}
			value := rune(c - '0')
			for count := 1; count < 3 && i+1 < len(raw); count++ {
				digit := raw[i+1]
				if digit < '0' || digit > '7' || value*8+rune(digit-'0') > 255 {
					break
				}
				value = value*8 + rune(digit-'0')
				i++
			}
			out = utf8.AppendRune(out, value)
		}
	}
	return string(out)
}

func hexDigit(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return -1
	}
}

func stringsContainsByte(s string, b byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return true
		}
	}
	return false
}

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
