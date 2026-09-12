package syntax_test

import (
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/v2/internal/text"
	"github.com/stretchr/testify/require"
)

func TestListAndDictionaryValues(t *testing.T) {
	tests := []struct {
		source string
		want   []string
	}{
		{" \t\n", []string{}},
		{`{} "" plain`, []string{"", "", "plain"}},
		{`{nested {braces}} "quoted space" escaped\ space`, []string{"nested {braces}", "quoted space", "escaped space"}},
		{`$version [command] {\n \t} {a\}b}`, []string{"$version", "[command]", `\n \t`, `a\}b`}},
		{`key value key later`, []string{"key", "value", "key", "later"}},
		{`a\;b c\\d`, []string{"a;b", `c\d`}},
		{"a\\\n  b", []string{"a b"}},
		{"\\\n  a", []string{" a"}},
		{"\"a\\\n  b\"", []string{"a b"}},
		{"{a\\\n  b}", []string{"a\\\n  b"}},
		{`\a\b\f\n\r\t\v`, []string{"\a\b\f\n\r\t\v"}},
		{`\101 \377 \400 \x41f \u00e9 \xFF \u00Af \U00000041 \x \q`, []string{"A", "ÿ", " 0", "Af", "é", "ÿ", "¯", "A", "x", "q"}},
	}
	for _, test := range tests {
		t.Run(test.source, func(t *testing.T) {
			values, errs := syntax.ListValues(test.source)
			require.Empty(t, errs, "ListValues(%q) = %q, %v; want %q", test.source, values, errs, test.want)
			require.Equal(t, test.want, values, "ListValues(%q) = %q, %v; want %q", test.source, values, errs, test.want)
		})
	}
	dictionary, errs := syntax.DictValues(`version 1 version 2 name {two words}`)
	require.Empty(t, errs, "dictionary: %v, %v", dictionary, errs)
	require.Equal(t, map[string]string{"version": "2", "name": "two words"}, dictionary, "dictionary: %v, %v", dictionary, errs)
}

func TestListLensesPreserveAbsoluteSourceSpans(t *testing.T) {
	source := []byte(`checksums {sha256 {nested value} "quoted value" escaped\ value}`)
	word := commands(parse(t, source))[0].Words[1]
	braced := word.Segments[0].(syntax.Braced)
	elements, errs := braced.ListLens(source)
	require.Empty(t, errs)

	want := []string{"sha256", "{nested value}", `"quoted value"`, `escaped\ value`}
	require.Len(t, elements, len(want), "elements: %v", elements)

	for i, element := range elements {
		checkSpan(t, element, braced.Body)
		wantText(t, source, element, want[i])
	}
}

func TestMalformedListsReturnDiagnosticsWithoutValues(t *testing.T) {
	for _, test := range []struct {
		source string
		kind   syntax.ErrorType
		offset int
	}{
		{"{open", syntax.ListUntermBrace, 0},
		{`"open`, syntax.ListUntermQuote, 0},
		{"{a}tail", syntax.ListElementNotSpaced, 3},
		{`"a"tail`, syntax.ListElementNotSpaced, 3},
		{"{a}\\\nb", syntax.ListElementNotSpaced, 3},
	} {
		t.Run(test.source, func(t *testing.T) {
			prefix := "ignored "
			source := []byte(prefix + test.source + " }")
			_, errs := syntax.SplitList(source, text.Span{Start: len(prefix), End: len(prefix) + len(test.source)})
			require.NotEmpty(t, errs, "diagnostic: %v", errs)
			require.Equal(t, test.kind, errs[0].Type, "diagnostic: %v", errs)
			require.Equal(t, len(prefix)+test.offset, errs[0].Span.Start, "diagnostic: %v", errs)

			values, errs := syntax.ListValues(test.source)
			require.NotEmpty(t, errs, "invalid list returned values: %v, %v", values, errs)
			require.Nil(t, values, "invalid list returned values: %v, %v", values, errs)

			dictionary, errs := syntax.DictValues(test.source)
			require.NotEmpty(t, errs, "invalid dictionary returned values: %v, %v", dictionary, errs)
			require.Nil(t, dictionary, "invalid dictionary returned values: %v, %v", dictionary, errs)
		})
	}
	values, errs := syntax.DictValues("key value dangling")
	require.Nil(t, values, "odd dictionary: %v, %v", values, errs)
	require.Len(t, errs, 1, "odd dictionary: %v, %v", values, errs)
	require.Equal(t, syntax.DictMissingValue, errs[0].Type, "odd dictionary: %v, %v", values, errs)
}
