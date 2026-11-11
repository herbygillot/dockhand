package term

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Prose wraps at the width under a hanging indent; a table's line, a word
// with nowhere to break, and a line not yet ended are written as they are.
func TestWrapped(t *testing.T) {
	require.Equal(t, "Next: one two\n  three", Wrapped("Next: one two three", 13))
	require.Equal(t, "  ! held: one\n    two three", Wrapped("  ! held: one two three", 14))
	require.Equal(t, "  jq  1.7.1  1.8.1  update", Wrapped("  jq  1.7.1  1.8.1  update", 10), "a table's")
	require.Equal(t, "see\n  "+strings.Repeat("x", 20), Wrapped("see "+strings.Repeat("x", 20), 10), "a long word overruns")
	require.Equal(t, "short", Wrapped("short", 0))

	var out bytes.Buffer
	w := &wrapWriter{w: &out, width: 10}
	_, _ = w.Write([]byte("? answer one two three "))
	_, _ = w.Write([]byte("now\nwrap these words here\n"))
	require.Equal(t, "? answer one two three now\nwrap these\n  words\n  here\n", out.String(), "a prompt, then lines")
}

// Out is never wrapped: a Portfile's 131-column modeline and a
// 300-column reinplace line come back byte for byte, on an 80-column
// stand-in terminal too, and a payload line that reads as a table isn't
// cut; the same lines to Prose wrap (#94).
func TestOutIsNeverWrapped(t *testing.T) {
	t.Parallel()
	modeline := "# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4"
	reinplace := "    reinplace \"s|@PKG_CPPFLAGS@|" + strings.Repeat("-I${prefix}/include/openbabel3 ", 9) + "|g\" ${worksrcpath}/src/Makevars.in"
	table := "long_description    " + strings.Repeat("a description that runs on ", 6)
	for _, streams := range []Streams{
		Begin(Streams{Out: &bytes.Buffer{}}.WithDisplay(Display{Columns: 80}), nil),
		Begin(Streams{Out: &bytes.Buffer{}}.AsTerminal().WithDisplay(Display{Columns: 80}), nil),
	} {
		out := underlying(streams.Out).(*bytes.Buffer)
		payload := modeline + "\n" + reinplace + "\n" + table + "\n"
		_, _ = streams.Out.Write([]byte(payload))
		require.Equal(t, payload, out.String(), "byte for byte")
		out.Reset()
		_, _ = streams.Prose().Write([]byte(modeline + "\n"))
		require.NotEqual(t, modeline+"\n", out.String(), "prose wraps")
		require.Contains(t, out.String(), "\n  ")
	}
}

// A stage holds both writers in the order written, says its own line
// through its Prose, and a command begun again, as the console runs one,
// is wrapped once.
func TestAStageHoldsBothWriters(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	streams := Begin(Streams{Out: &out}.WithDisplay(Display{Columns: 20}), nil)
	staged, stage := streams.Staged("editing")
	_, _ = staged.Prose().Write([]byte("a sentence of the stage's own words\n"))
	_, _ = staged.Out.Write([]byte("raw payload that runs past twenty columns\n"))
	require.Empty(t, out.String(), "held")
	stage.Say()
	require.Equal(t, "a sentence of the\n  stage's own words\nraw payload that runs past twenty columns\n", out.String())

	out.Reset()
	again := Begin(streams, nil)
	_, _ = again.Out.Write([]byte("raw payload that runs past twenty columns\n"))
	require.Equal(t, "raw payload that runs past twenty columns\n", out.String(), "begun again, not wrapped twice")
	out.Reset()
	_, stage = streams.Staged("committing")
	stage.Done("✓ committed a line that is long enough to wrap")
	require.Equal(t, "✓ committed a line\n  that is long\n  enough to wrap\n", out.String(), "the stage's own line, wrapped")
}

// On a terminal a table's line is cut at the width with "…", at the
// default level; -v shows it whole.
func TestCut(t *testing.T) {
	require.Equal(t, "  jq  1.7.1 → 1.8.1  a long n…", Cut("  jq  1.7.1 → 1.8.1  a long note that runs on", 30))
	require.Equal(t, "prose that runs on past the width", Cut("prose that runs on past the width", 10), "prose wraps instead")

	var out bytes.Buffer
	streams := Begin(Streams{Out: &out}.AsTerminal().WithDisplay(Display{Columns: 20}), nil)
	_, _ = streams.Prose().Write([]byte("  jq  update  a long note\n"))
	streams.SetVerbosity(1)
	_, _ = streams.Prose().Write([]byte("  jq  update  a long note\n"))
	require.Equal(t, "  jq  update  a lon…\n  jq  update  a long note\n", out.String())
}

// A stamp installed beneath --json's switch reaches prose and raw lines
// alike, where serve's, put over Out, missed what Prose wrote (#94).
func TestAStampReachesBothWriters(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	streams := Begin(Streams{Out: &out}, nil).Stamped(func(w io.Writer) io.Writer { return prefixWriter{w} })
	_, _ = streams.Prose().Write([]byte("serve: waiting\n"))
	_, _ = streams.Out.Write([]byte("a log line\n"))
	require.Equal(t, "> serve: waiting\n> a log line\n", out.String())
}

// prefixWriter starts each write with "> ", as a stand-in stamp.
type prefixWriter struct{ w io.Writer }

func (p prefixWriter) Write(b []byte) (int, error) {
	_, err := p.w.Write(append([]byte("> "), b...))
	return len(b), err
}
