package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wordAt parses src and returns the given word of the given command.
func wordAt(t *testing.T, src string, cmd, word int) Word {
	t.Helper()
	sc, errs := Parse([]byte(src))
	require.Empty(t, errs)
	c, ok := sc.Items[cmd].(Command)
	require.True(t, ok)
	require.Greater(t, len(c.Words), word)
	return c.Words[word]
}

func TestWordLiteral(t *testing.T) {
	cases := []struct {
		src  string
		text string
		ok   bool
	}{
		{`version 1.2.3`, "1.2.3", true},
		{`version "1.2.3"`, "", false},
		{`version {1.2.3}`, "", false},
		{`version $v`, "", false},
		{`version 1.$v`, "", false},
		{`version [get]`, "", false},
		{`version {*}1.2.3`, "", false},
	}
	for _, c := range cases {
		got, ok := wordAt(t, c.src, 0, 1).Literal([]byte(c.src))
		assert.Equal(t, c.ok, ok, "src=%q", c.src)
		assert.Equal(t, c.text, got, "src=%q", c.src)
	}
}

func TestWordBracedScript(t *testing.T) {
	src := `if {$cond} {
    version 1.2.3
}`
	body, ok := wordAt(t, src, 0, 2).BracedScript([]byte(src))
	require.True(t, ok)
	require.Len(t, body.Items, 1)
	inner, ok := body.Items[0].(Command)
	require.True(t, ok)
	name, ok := inner.Name([]byte(src))
	require.True(t, ok)
	assert.Equal(t, "version", name)

	// Not a braced word.
	_, ok = wordAt(t, src, 0, 0).BracedScript([]byte(src))
	assert.False(t, ok)

	// A {*}-expanded braced word is a list being spliced, not a script.
	expand := `run {*}{a b}`
	_, ok = wordAt(t, expand, 0, 1).BracedScript([]byte(expand))
	assert.False(t, ok)

	// A body that does not parse cleanly as a script.
	bad := `run { "unterminated }`
	_, ok = wordAt(t, bad, 0, 1).BracedScript([]byte(bad))
	assert.False(t, ok)
}
