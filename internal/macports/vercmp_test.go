package macports

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

func TestVerCmp(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0", "1.0", 0},
		{"1.10", "1.9", 1},   // numeric, not lexical
		{"1.0", "1.0.1", -1}, // more segments is newer
		{"1.0", "1.0.0", -1},
		{"2.36.0", "2.35.0", 1},
		{"0.5.8", "0.4.0", 1},
		{"1.0", "1.0a", -1},  // trailing alpha is newer than nothing
		{"1.0a", "1.0b", -1}, // alpha ordered lexically
		// The famous gotcha: MacPorts has no prerelease awareness, so
		// trailing content wins and 1.0rc1 sorts NEWER than 1.0 — the
		// reason maintainers avoid rc-suffixed versions.
		{"1.0rc1", "1.0", 1},
		{"1_0", "1.0", 0},      // separators don't distinguish
		{"01.2", "1.2", 0},     // leading zeros don't count
		{"1.02", "1.2", 0},     // ...anywhere
		{"20260223", "1.0", 1}, // date-style versions are just numbers
		{"1.2.3", "1.2.10", -1},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sign(VerCmp(c.a, c.b)), "%s vs %s", c.a, c.b)
		assert.Equal(t, -c.want, sign(VerCmp(c.b, c.a)), "%s vs %s reversed", c.b, c.a)
	}
}
