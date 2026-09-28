package syntax

import "strings"

// Quote is value written as one Tcl word, which Tcl reads back as exactly
// value, as Tcl's own list quoting writes an element:
//   - bare, where nothing in it means anything to Tcl;
//   - braced, where braces can hold it: they balance, and no backslash
//     ends it or comes before a newline, which Tcl would read even inside
//     braces;
//   - otherwise with each character that means something backslashed.
//
// A leading # is quoted too, so the word can stand anywhere in a command.
func Quote(value string) string {
	if value == "" {
		return "{}"
	}
	special, braceable := false, true
	depth := 0
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '{':
			special = true
			depth++
		case '}':
			special = true
			depth--
			if depth < 0 {
				braceable = false
			}
		case '\\':
			special = true
			if i+1 == len(value) || value[i+1] == '\n' {
				braceable = false
			}
			// An escaped brace doesn't count toward balancing, inside
			// braces as out.
			i++
		case '[', ']', '$', ';', '"', ' ', '\t', '\n', '\r', '\f', '\v':
			special = true
		case '#':
			special = special || i == 0
		}
	}
	switch {
	case !special:
		return value
	case braceable && depth == 0:
		return "{" + value + "}"
	}
	var b strings.Builder
	for i, r := range value {
		switch r {
		case '{', '}', '[', ']', '$', ';', '"', '\\', ' ':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\f':
			b.WriteString(`\f`)
		case '\v':
			b.WriteString(`\v`)
		case '#':
			if i == 0 {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
