package syntax_test

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
	"github.com/stretchr/testify/require"
)

func checkSpan(t *testing.T, span, parent text.Span) {
	t.Helper()
	require.GreaterOrEqual(t, span.Start, parent.Start, "span %+v outside %+v", span, parent)
	require.GreaterOrEqual(t, span.End, span.Start, "span %+v outside %+v", span, parent)
	require.LessOrEqual(t, span.End, parent.End, "span %+v outside %+v", span, parent)
}

func checkScript(t *testing.T, source []byte, script *syntax.Script, parent text.Span) {
	t.Helper()
	require.NotNil(t, script, "nil script")

	checkSpan(t, script.Span, parent)
	end := script.Span.Start
	for _, item := range script.Items {
		span := syntax.SpanOf(item)
		checkSpan(t, span, script.Span)
		require.GreaterOrEqual(t, span.Start, end, "unordered or empty item: %+v after %d", span, end)
		require.NotEqual(t, 0, span.Len(), "unordered or empty item: %+v after %d", span, end)

		end = span.End
		if command, ok := item.(syntax.Command); ok {
			require.NotEmpty(t, command.Words, "empty command")

			wordEnd := command.Span.Start
			for _, word := range command.Words {
				checkSpan(t, word.Span, command.Span)
				require.GreaterOrEqual(t, word.Span.Start, wordEnd, "unordered or empty word: %+v after %d", word.Span, wordEnd)
				require.NotEqual(t, 0, word.Span.Len(), "unordered or empty word: %+v after %d", word.Span, wordEnd)

				wordEnd = word.Span.End
				checkSegments(t, source, word.Segments, word.Span)
			}
		}
	}
}

func checkSegments(t *testing.T, source []byte, segments []syntax.Segment, parent text.Span) {
	t.Helper()
	end := parent.Start
	for _, segment := range segments {
		span := syntax.SegmentSpan(segment)
		checkSpan(t, span, parent)
		require.GreaterOrEqual(t, span.Start, end, "overlapping segments: %+v after %d", span, end)

		end = span.End
		switch segment := segment.(type) {
		case syntax.VarSub:
			checkSpan(t, segment.Name, span)
			if segment.HasIndex {
				checkSpan(t, segment.Index, span)
			}
		case syntax.CmdSub:
			checkScript(t, source, segment.Script, span)
		case syntax.Braced:
			checkSpan(t, segment.Body, span)
		case syntax.Quoted:
			checkSegments(t, source, segment.Segments, span)
		}
	}
}

func FuzzParse(f *testing.F) {
	for _, source := range []string{portfile, "", "[", `set a $b([list {]}])`, `cmd {*}$args`, "{*}\\\n next", "set a {open", "[set a \"$\" ]", "\x00\xff\xc3"} {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, source []byte) {
		if len(source) > 4096 {
			t.Skip()
		}
		window := text.Span{End: len(source)}
		script, errs := syntax.Parse(source)
		checkScript(t, source, script, window)
		for _, err := range errs {
			checkSpan(t, err.Span, window)
		}
		if len(source) != 0 {
			window.Start = int(source[0]) % (len(source) + 1)
			window.End = window.Start + int(source[len(source)-1])%(len(source)-window.Start+1)
			script, errs = syntax.ParseScript(source, window)
			checkScript(t, source, script, window)
			for _, err := range errs {
				checkSpan(t, err.Span, window)
			}
		}
	})
}

func FuzzSplitList(f *testing.F) {
	for _, source := range []string{"", `{a b} "c d" e\ f`, "a\\\n  b", `{}x`, `"a`, "\x00\xff"} {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, source []byte) {
		if len(source) > 4096 {
			t.Skip()
		}
		window := text.Span{End: len(source)}
		elements, errs := syntax.SplitList(source, window)
		end := window.Start
		for _, element := range elements {
			checkSpan(t, element, window)
			require.GreaterOrEqual(t, element.Start, end, "unordered or empty list element: %+v after %d", element, end)
			require.NotEqual(t, 0, element.Len(), "unordered or empty list element: %+v after %d", element, end)

			end = element.End
		}
		for _, err := range errs {
			checkSpan(t, err.Span, window)
		}
		if len(errs) == 0 {
			values, valueErrors := syntax.ListValues(string(source))
			require.Empty(t, valueErrors, "list values disagree with parsed elements")
			require.Len(t, values, len(elements), "list values disagree with parsed elements")
		}
	})
}
