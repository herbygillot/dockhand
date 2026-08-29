package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestListValue(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"bare", "bare"},
		{"{a b}", "a b"},
		{"{}", ""},
		{`"a b"`, "a b"},
		{`a\ b`, "a b"},
		{`\{`, "{"},
		{`\}`, "}"},
		{`\\`, `\`},
		{`{@alice example.com:alice}`, "@alice example.com:alice"},
		{"{nested {inner} kept}", "nested {inner} kept"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, ListValue(c.raw), "ListValue(%q)", c.raw)
	}
}

func TestListValues(t *testing.T) {
	got, errs := ListValues("a {b c} d")
	assert.Empty(t, errs)
	assert.Equal(t, []string{"a", "b c", "d"}, got)

	_, errs = ListValues("{unterminated")
	assert.NotEmpty(t, errs)
}

func TestDictValues(t *testing.T) {
	got, errs := DictValues("name libftdi subports {libftdi0 libftdi1}")
	assert.Empty(t, errs)
	assert.Equal(t, map[string]string{"name": "libftdi", "subports": "libftdi0 libftdi1"}, got)

	got, errs = DictValues("")
	assert.Empty(t, errs)
	assert.Empty(t, got)

	_, errs = DictValues("a b c")
	assert.Len(t, errs, 1)
	assert.Equal(t, DictMissingValue, errs[0].Type)
}
