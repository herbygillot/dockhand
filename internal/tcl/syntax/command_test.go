package syntax

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func texts(src []byte, words []Word) []string {
	var out []string
	for _, word := range words {
		out = append(out, word.Span.Text(src))
	}
	return out
}

// Every reader that needs to know what an if, a loop, or a switch selects
// and what it runs asks here, so the shape of each is written once.
func TestControlSeparatesSelectorsFromBodies(t *testing.T) {
	t.Parallel()
	src := []byte("if {$a} then {x} elseif {$b} {y} else {z}\n" +
		"foreach {k v} $list {w}\n" +
		"while {$i} {u}\n" +
		"for {set i 0} {$i < 3} {incr i} {v}\n" +
		"catch {t} err\n" +
		"switch -exact -- $s {\n    a -\n    b {p}\n    default {q}\n}\n" +
		"switch $s a {r} b {m}\n" +
		"version 1.0\n" +
		"if {$a} {b} elseif\n" +
		"switch $s\n" +
		"switch $s {a {r} b}\n")
	script, errs := Parse(src)
	require.Empty(t, errs)
	commands := script.Direct()
	require.Len(t, commands, 11)

	controls, bodies, ok := commands[0].Control(src)
	require.True(t, ok)
	require.Equal(t, []string{"{$a}", "{$b}"}, texts(src, controls))
	require.Equal(t, []string{"{x}", "{y}", "{z}"}, texts(src, bodies), "then and else are keywords, not bodies")

	controls, bodies, ok = commands[1].Control(src)
	require.True(t, ok)
	require.Equal(t, []string{"{k v}", "$list"}, texts(src, controls))
	require.Equal(t, []string{"{w}"}, texts(src, bodies))

	controls, bodies, ok = commands[2].Control(src)
	require.True(t, ok)
	require.Equal(t, []string{"{$i}"}, texts(src, controls))
	require.Equal(t, []string{"{u}"}, texts(src, bodies))

	controls, bodies, ok = commands[3].Control(src)
	require.True(t, ok)
	require.Equal(t, []string{"{$i < 3}"}, texts(src, controls))
	require.Equal(t, []string{"{set i 0}", "{incr i}", "{v}"}, texts(src, bodies), "a for's start and next are scripts too")

	controls, bodies, ok = commands[4].Control(src)
	require.True(t, ok)
	require.Empty(t, controls)
	require.Equal(t, []string{"{t}"}, texts(src, bodies))

	controls, bodies, ok = commands[5].Control(src)
	require.True(t, ok)
	require.Equal(t, []string{"-exact", "--", "$s", "a", "b", "default"}, texts(src, controls), "options, the value, and the patterns select")
	require.Equal(t, []string{"{p}", "{q}"}, texts(src, bodies), "a - falls through and is not a body")

	controls, bodies, ok = commands[6].Control(src)
	require.True(t, ok)
	require.Equal(t, []string{"$s", "a", "b"}, texts(src, controls))
	require.Equal(t, []string{"{r}", "{m}"}, texts(src, bodies), "the inline form pairs after the value")

	_, _, ok = commands[7].Control(src)
	require.False(t, ok, "an ordinary command is not a control")
	_, _, ok = commands[8].Control(src)
	require.False(t, ok, "an if still waiting for a condition is not one either")
	_, _, ok = commands[9].Control(src)
	require.False(t, ok, "a switch with no arms is Tcl's error, not a control")
	_, _, ok = commands[10].Control(src)
	require.False(t, ok, "and so is a pattern with no body")
}

func TestWordAndCommandPredicates(t *testing.T) {
	t.Parallel()
	src := []byte("ui_error \"no ${name} here\"\nreturn -code error [bad]\nset x {1.2}\nset y \"1.3\"\nfoo a b[c]\nbar a b\nbaz $v(i)\n")
	script, errs := Parse(src)
	require.Empty(t, errs)
	commands := script.Direct()
	require.Len(t, commands, 7)

	require.True(t, commands[0].Words[1].Plain(), "text and a simple variable")
	require.False(t, commands[1].Words[3].Plain(), "a command substitution")
	require.False(t, commands[6].Words[1].Plain(), "an indexed variable")

	require.True(t, commands[1].Is(src, "return", "-code", "error", "[bad]"))
	require.False(t, commands[1].Is(src, "return", "-code", "error"), "the count must match")
	require.True(t, Command{Words: commands[1].Words[:3]}.Is(src, "return", "-code", "error"), "a prefix can be compared as its own command")

	args, ok := commands[5].LiteralArgs(src)
	require.True(t, ok)
	require.Equal(t, []string{"a", "b"}, args)
	_, ok = commands[4].LiteralArgs(src)
	require.False(t, ok, "a substitution is not a literal argument")
	_, ok = commands[2].LiteralArgs(src)
	require.False(t, ok, "a braced argument is not a bare literal")

	require.Equal(t, "1.2", commands[2].Words[2].Inner().Text(src))
	require.Equal(t, "1.3", commands[3].Words[2].Inner().Text(src))
	require.Equal(t, "a", commands[5].Words[1].Inner().Text(src), "a bare word is its own inner")
}

// -matchvar and -indexvar each take the variable the switch writes, so the
// value switched on is the word after it, not the variable.
func TestControlReadsASwitchVariableAsItsOptionsArgument(t *testing.T) {
	t.Parallel()
	src := []byte("switch -regexp -indexvar where -matchvar found -- $s {^a {x}}\nswitch -matchvar\n")
	script, errs := Parse(src)
	require.Empty(t, errs)
	commands := script.Direct()
	controls, bodies, ok := commands[0].Control(src)
	require.True(t, ok)
	require.Equal(t, []string{"-regexp", "-indexvar", "where", "-matchvar", "found", "--", "$s", "^a"}, texts(src, controls))
	require.Equal(t, []string{"{x}"}, texts(src, bodies))
	require.True(t, SwitchWritesVariable("-indexvar"))
	require.False(t, SwitchWritesVariable("-regexp"))
	_, _, ok = commands[1].Control(src)
	require.False(t, ok, "an option missing its variable is no control structure")
}
