package syntax_test

import (
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/v2/internal/text"
	"github.com/stretchr/testify/require"
)

const portfile = `# A computed version; source must remain editable.
set release {1.9.2}
version [string map {- .} [format "%s-%s" $release [expr {2 + 1}]]]
distname "café-${name}-$::release[set suffix]"
checksums {*}$checksum_values
if {$enabled} {
    revision 1
    description {version 999}
}
`

func parse(t *testing.T, source []byte) *syntax.Script {
	t.Helper()
	script, errs := syntax.Parse(source)
	require.Empty(t, errs, "Parse(%q): %v", source, errs)

	checkScript(t, source, script, text.Span{End: len(source)})
	return script
}

func commands(script *syntax.Script) []syntax.Command {
	var result []syntax.Command
	for _, item := range script.Items {
		if command, ok := item.(syntax.Command); ok {
			result = append(result, command)
		}
	}
	return result
}

func wantText(t *testing.T, source []byte, span text.Span, want string) {
	t.Helper()
	got := span.Text(source)
	require.Equal(t, want, got, "source[%d:%d] = %q, want %q", span.Start, span.End, got, want)
}

func TestParseComputedPortfilePreservesSyntaxAndSource(t *testing.T) {
	source := []byte(portfile)
	script := parse(t, source)
	require.Equal(t, portfile, string(source), "parser modified source")

	_, ok := script.Items[0].(syntax.Comment)
	require.True(t, ok, "lost initial comment")

	top := commands(script)
	var names []string
	for _, command := range top {
		name, ok := command.Name(source)
		require.True(t, ok, "plain command name treated as dynamic")

		names = append(names, name)
	}
	require.Equal(t, []string{"set", "version", "distname", "checksums", "if"}, names, "top-level commands: %v", names)

	value, ok := top[0].Words[1].Literal(source)
	require.True(t, ok, "literal: %q, %v", value, ok)
	require.Equal(t, "release", value, "literal: %q, %v", value, ok)

	_, ok = top[1].Words[1].Literal(source)
	require.False(t, ok, "computed version reported as literal")

	outer := top[1].Words[1].Segments[0].(syntax.CmdSub)
	stringMap := commands(outer.Script)[0]
	wantText(t, source, stringMap.Span, `string map {- .} [format "%s-%s" $release [expr {2 + 1}]]`)
	format := commands(stringMap.Words[3].Segments[0].(syntax.CmdSub).Script)[0]
	release := format.Words[2].Segments[0].(syntax.VarSub)
	wantText(t, source, release.Name, "release")
	expression := commands(format.Words[3].Segments[0].(syntax.CmdSub).Script)[0]
	wantText(t, source, expression.Words[1].Segments[0].(syntax.Braced).Body, "2 + 1")
	quoted := top[2].Words[1].Segments[0].(syntax.Quoted)
	require.Len(t, quoted.Segments, 5, "quoted segments: %#v", quoted.Segments)

	wantText(t, source, quoted.Segments[0].(syntax.Literal).Span, "café-")
	wantText(t, source, quoted.Segments[1].(syntax.VarSub).Name, "name")
	wantText(t, source, quoted.Segments[3].(syntax.VarSub).Name, "::release")
	expanded := top[3].Words[1]
	require.True(t, expanded.Expand, "lost argument expansion")

	wantText(t, source, expanded.Span, "{*}$checksum_values")
	_, ok = expanded.Literal(source)
	require.False(t, ok, "expanded word reported as literal")

	body, ok := top[4].Words[2].BracedScript(source)
	require.True(t, ok, "cannot inspect conditional body")
	require.Len(t, commands(body), 2, "cannot inspect conditional body")
}

func TestParseQuotingVariablesAndCommandBoundaries(t *testing.T) {
	tests := []struct {
		source string
		words  []string
	}{
		{"set x a\\ b", []string{"set", "x", "a\\ b"}},
		{"set x one\\\n  two", []string{"set", "x", "one", "two"}},
		{`set x {a {b} \} $literal [opaque]}`, []string{"set", "x", `{a {b} \} $literal [opaque]}`}},
		{`set x "escaped \" and \$literal; ]"`, []string{"set", "x", `"escaped \" and \$literal; ]"`}},
		{`set x [list {]} "]" [list nested]]`, []string{"set", "x", `[list {]} "]" [list nested]]`}},
		{`set x #literal`, []string{"set", "x", "#literal"}},
		{`cmd {*} {*}{a b} {*}[list c]`, []string{"cmd", "{*}", "{*}{a b}", "{*}[list c]"}},
		{`set x pre$::ns::v${odd-name}$arr([list a(b)])post`, []string{"set", "x", `pre$::ns::v${odd-name}$arr([list a(b)])post`}},
	}
	for _, test := range tests {
		t.Run(test.source, func(t *testing.T) {
			source := []byte(test.source)
			top := commands(parse(t, source))
			require.Len(t, top, 1, "commands: %d", len(top))

			var words []string
			for _, word := range top[0].Words {
				words = append(words, word.Span.Text(source))
			}
			require.Equal(t, test.words, words, "words: %q, want %q", words, test.words)
		})
	}
	source := []byte(`set value $::ns::v${odd-name}$arr([list a(b)])$(empty-name)$`)
	segments := commands(parse(t, source))[0].Words[2].Segments
	wantText(t, source, segments[0].(syntax.VarSub).Name, "::ns::v")
	wantText(t, source, segments[1].(syntax.VarSub).Name, "odd-name")
	array := segments[2].(syntax.VarSub)
	require.True(t, array.HasIndex, "array treated as scalar")

	wantText(t, source, array.Index, "[list a(b)]")
	emptyName := segments[3].(syntax.VarSub)
	require.True(t, emptyName.HasIndex, "empty array name was lost")
	require.Zero(t, emptyName.Name.Len(), "empty array name was lost")

	wantText(t, source, segments[4].(syntax.Literal).Span, "$")
	source = []byte("# first\\\n still a comment\nset x 1; # second\nset y 2")
	script := parse(t, source)
	require.Len(t, script.Items, 4, "comment/semicolon boundaries: %#v", script.Items)
	require.Len(t, commands(script), 2, "comment/semicolon boundaries: %#v", script.Items)

	wantText(t, source, syntax.SpanOf(script.Items[0]), "# first\\\n still a comment")
}

func TestParseWindowsAndErrorRecovery(t *testing.T) {
	for _, test := range []struct {
		body  string
		kind  syntax.ErrorType
		start int
	}{
		{"set x {open", syntax.UntermBrace, 6},
		{`set x "open`, syntax.UntermQuote, 6},
		{`set x ${open`, syntax.UntermVarName, 6},
		{`set x $a(open`, syntax.UntermArrayIndex, 6},
		{`set x [open`, syntax.UntermCmdSub, 6},
		{"set x {a}tail\nversion 2", syntax.ExtraAfterCloseBrace, 9},
		{"set x \"a\"tail\nversion 2", syntax.ExtraAfterCloseQuote, 9},
	} {
		t.Run(test.kind.String(), func(t *testing.T) {
			prefix := "outside\n"
			source := []byte(prefix + test.body + " } ] \" suffix")
			window := text.Span{Start: len(prefix), End: len(prefix) + len(test.body)}
			script, errs := syntax.ParseScript(source, window)
			require.Len(t, errs, 1, "diagnostics: %v", errs)
			require.Equal(t, test.kind, errs[0].Type, "diagnostics: %v", errs)
			require.Equal(t, len(prefix)+test.start, errs[0].Span.Start, "diagnostics: %v", errs)
			require.Equal(t, window, script.Span, "script escaped requested window: %+v", script.Span)

			checkScript(t, source, script, window)
			require.True(t, strings.HasPrefix(errs[0].Describe(source), "2:"), "diagnostic lost original line: %s", errs[0].Describe(source))
			if strings.Contains(test.body, "version 2") {
				require.Len(t, commands(script), 2, "error recovery swallowed following command")
			}
		})
	}
}

func TestCommandsDescendOnlyWhereRequestedAndStopEarly(t *testing.T) {
	source := []byte("if 1 {version 2; if 1 {revision 3}}\ndescription {version 999}\nversion [format %s 4]")
	script := parse(t, source)
	descend := func(command syntax.Command) bool { name, _ := command.Name(source); return name == "if" }
	var names []string
	for command := range script.Commands(source, descend) {
		name, _ := command.Name(source)
		names = append(names, name)
	}
	require.Equal(t, []string{"if", "version", "if", "revision", "description", "version"}, names, "selective traversal: %v", names)

	calls := 0
	for range script.Commands(source, func(syntax.Command) bool { calls++; return true }) {
		break
	}
	require.Zero(t, calls, "iterator continued after consumer stopped")

	source = []byte(`command {*}{version 2} {"unfinished} "version 3"`)
	top := commands(parse(t, source))[0]
	for _, word := range top.Words[1:] {
		_, ok := word.BracedScript(source)
		require.False(t, ok, "unsafe braced-script lens accepted %q", word.Span.Text(source))
	}
}
