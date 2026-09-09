package bump

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// THE AFFIXES BRACKET WHERE THE PROBE LANDED, and that bracketing is the
// whole proof: everything outside it is text the version keeps whatever
// the literal says, and everything inside it is the literal's
// contribution.
func TestAffixesBracketTheProbedLiteral(t *testing.T) {
	for _, c := range []struct{ a, b, pre, suf string }{
		// terraform: the literal is the whole tail
		{"1.16.0", "1.16.7", "1.16.", ""},
		// a leading component: msp430-gdb's version_base
		{"7.2a-20111205", "4.7q-20111205", "", "-20111205"},
		// the ordinary case: the literal IS the version
		{"2.13.1", "7.74.7", "", ""},
		// a middle component
		{"1.2.3-rc", "1.7.3-rc", "1.", ".3-rc"},
	} {
		pre, suf := affixes(c.a, c.b)
		assert.Equal(t, c.pre, pre, "prefix of %q/%q", c.a, c.b)
		assert.Equal(t, c.suf, suf, "suffix of %q/%q", c.a, c.b)
	}
}

// THE PREFIX AND SUFFIX MAY NOT OVERLAP, which a version of one repeated
// character would otherwise make them do: "111" against "171" shares a
// prefix "1" and a suffix "1", and a naive pair would claim both ends of
// a string with one character between them.
func TestAffixesDoNotOverlap(t *testing.T) {
	pre, suf := affixes("111", "171")
	assert.Equal(t, "1", pre)
	assert.Equal(t, "1", suf)
	assert.LessOrEqual(t, len(pre)+len(suf), len("111"), "the middle cannot be negative")

	pre, suf = affixes("77", "77")
	assert.LessOrEqual(t, len(pre)+len(suf), len("77"))
}

// A PROBE KEEPS THE LITERAL'S SHAPE, so a Portfile that compares or
// computes with its version still evaluates. A sentinel like "ZZZZ"
// would be easier to spot in the output and would break exactly the
// ports this exists to serve — the ones that do something with the
// value rather than just printing it.
func TestAProbeKeepsTheLiteralsShape(t *testing.T) {
	for _, c := range []struct{ lit, want string }{
		{"0", "7"},
		{"7", "4"},
		{"1.16.0", "7.77.7"},
		{"2024-03-21", "7777-77-77"}, // no 7s to dodge, so every digit moves
		{"2_5_3", "7_7_7"},
		{"pre21", "qqq77"},
	} {
		got := probeValue(c.lit)
		assert.Equal(t, c.want, got, "probe for %q", c.lit)
		assert.NotEqual(t, c.lit, got, "a probe equal to the literal proves nothing")
		assert.Len(t, got, len(c.lit), "same shape means same length")
	}
	assert.Empty(t, probeValue(""), "nothing to probe")
}
