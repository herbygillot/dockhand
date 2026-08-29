package syntax

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The concatenation invariant, enforced structurally: every node's children
// tile the node's span, and the bytes between siblings are exactly the
// separator trivia the grammar allows there. A tree that passes reproduces
// the input byte-for-byte.

func verifyScript(t *testing.T, src []byte, s *Script, lo, hi int) {
	t.Helper()
	prev := lo
	for _, it := range s.Items {
		sp := SpanOf(it)
		if sp.Start < prev || sp.End > hi {
			t.Fatalf("item span %v outside [%d,%d) or overlapping", sp, prev, hi)
		}
		verifyScriptGap(t, src, prev, sp.Start)
		switch it := it.(type) {
		case Command:
			verifyCommand(t, src, it)
		case Comment:
			// span only
		}
		prev = sp.End
	}
	verifyScriptGap(t, src, prev, hi)
}

// verifyScriptGap checks bytes between commands: whitespace, newlines,
// semicolons, and backslash-newline pairs.
func verifyScriptGap(t *testing.T, src []byte, lo, hi int) {
	t.Helper()
	for i := lo; i < hi; {
		c := src[i]
		switch {
		case isSpace(c) || c == '\n' || c == ';':
			i++
		case c == '\\' && i+1 < hi && src[i+1] == '\n':
			i += 2
		default:
			t.Fatalf("unexpected byte %q at %d in inter-command gap [%d,%d)", c, i, lo, hi)
		}
	}
}

// verifyWordGap checks bytes between words of one command: spaces, tabs,
// and backslash-newline. A bare newline or semicolon here is a bug.
func verifyWordGap(t *testing.T, src []byte, lo, hi int) {
	t.Helper()
	for i := lo; i < hi; {
		c := src[i]
		switch {
		case isSpace(c):
			i++
		case c == '\\' && i+1 < hi && src[i+1] == '\n':
			i += 2
		default:
			t.Fatalf("unexpected byte %q at %d in word gap [%d,%d)", c, i, lo, hi)
		}
	}
}

func verifyCommand(t *testing.T, src []byte, c Command) {
	t.Helper()
	if len(c.Words) == 0 {
		t.Fatalf("command with no words at %v", c.Span)
	}
	if c.Span.Start != c.Words[0].Span.Start || c.Span.End != c.Words[len(c.Words)-1].Span.End {
		t.Fatalf("command span %v does not match its words", c.Span)
	}
	prev := c.Span.Start
	for _, w := range c.Words {
		verifyWordGap(t, src, prev, w.Span.Start)
		verifyWord(t, src, w)
		prev = w.Span.End
	}
}

func verifyWord(t *testing.T, src []byte, w Word) {
	t.Helper()
	segStart := w.Span.Start
	if w.Expand {
		if string(src[w.Span.Start:w.Span.Start+3]) != "{*}" {
			t.Fatalf("expand word at %v does not start with {*}", w.Span)
		}
		segStart += 3
	}
	prev := segStart
	for _, seg := range w.Segments {
		sp := SegmentSpan(seg)
		if sp.Start != prev {
			t.Fatalf("segment gap in word %v: expected start %d, got %v", w.Span, prev, sp)
		}
		verifySegment(t, src, seg)
		prev = sp.End
	}
	if prev != w.Span.End {
		t.Fatalf("segments of word %v end at %d, want %d", w.Span, prev, w.Span.End)
	}
}

func verifySegment(t *testing.T, src []byte, seg Segment) {
	t.Helper()
	switch seg := seg.(type) {
	case Literal, VarSub:
		// span only
	case Braced:
		if src[seg.Span.Start] != '{' {
			t.Fatalf("braced segment at %v does not start with brace", seg.Span)
		}
	case Quoted:
		if src[seg.Span.Start] != '"' {
			t.Fatalf("quoted segment at %v does not start with quote", seg.Span)
		}
		inner := seg.Span.End
		if src[seg.Span.End-1] == '"' && seg.Span.End-1 > seg.Span.Start {
			inner = seg.Span.End - 1
		}
		prev := seg.Span.Start + 1
		for _, s := range seg.Segments {
			sp := SegmentSpan(s)
			if sp.Start != prev {
				t.Fatalf("segment gap in quoted %v at %d", seg.Span, prev)
			}
			verifySegment(t, src, s)
			prev = sp.End
		}
		if prev != inner {
			t.Fatalf("quoted %v inner segments end at %d, want %d", seg.Span, prev, inner)
		}
	case CmdSub:
		if src[seg.Span.Start] != '[' {
			t.Fatalf("cmdsub at %v does not start with bracket", seg.Span)
		}
		verifyScript(t, src, seg.Script, seg.Script.Span.Start, seg.Script.Span.End)
	}
}

func parseVerified(t *testing.T, src string) (*Script, []Error) {
	t.Helper()
	s, errs := Parse([]byte(src))
	verifyScript(t, []byte(src), s, 0, len(src))
	return s, errs
}

func mustNoErrors(t *testing.T, errs []Error) {
	t.Helper()
	require.Empty(t, errs)
}

func commands(s *Script) []Command {
	var out []Command
	for _, it := range s.Items {
		if c, ok := it.(Command); ok {
			out = append(out, c)
		}
	}
	return out
}

func TestPortgroupSetupLine(t *testing.T) {
	src := "go.setup            github.com/robpike/ivy 0.4.0 v\n"
	s, errs := parseVerified(t, src)
	mustNoErrors(t, errs)
	cmds := commands(s)
	if len(cmds) != 1 {
		t.Fatalf("want 1 command, got %d", len(cmds))
	}
	c := cmds[0]
	if name, ok := c.Name([]byte(src)); !ok || name != "go.setup" {
		t.Fatalf("command name = %q, %v", name, ok)
	}
	if len(c.Words) != 4 {
		t.Fatalf("want 4 words, got %d", len(c.Words))
	}
	if got := c.Words[2].Span.Text([]byte(src)); got != "0.4.0" {
		t.Fatalf("version word = %q", got)
	}
}

func TestMaintainersBracedList(t *testing.T) {
	src := "maintainers         {@alice example.com:alice} openmaintainer\n"
	s, errs := parseVerified(t, src)
	mustNoErrors(t, errs)
	c := commands(s)[0]
	if len(c.Words) != 3 {
		t.Fatalf("want 3 words, got %d", len(c.Words))
	}
	braced, ok := c.Words[1].Segments[0].(Braced)
	if !ok {
		t.Fatalf("word 1 is not braced")
	}
	elems, lerr := braced.ListLens([]byte(src))
	if len(lerr) != 0 || len(elems) != 2 {
		t.Fatalf("list lens: %d elems, issues %v", len(elems), lerr)
	}
	if got := elems[0].Text([]byte(src)); got != "@alice" {
		t.Fatalf("first element = %q", got)
	}
}

func TestLineContinuation(t *testing.T) {
	src := "checksums           rmd160  abc \\\n                    sha256  def\n"
	s, errs := parseVerified(t, src)
	mustNoErrors(t, errs)
	cmds := commands(s)
	if len(cmds) != 1 {
		t.Fatalf("continuation split the command: got %d", len(cmds))
	}
	if len(cmds[0].Words) != 5 {
		t.Fatalf("want 5 words, got %d", len(cmds[0].Words))
	}
}

func TestComputedVersionLine(t *testing.T) {
	src := "version             20251208-4.2.1-[string range ${github.version} 0 7]\n"
	s, errs := parseVerified(t, src)
	mustNoErrors(t, errs)
	c := commands(s)[0]
	if len(c.Words) != 2 {
		t.Fatalf("want 2 words, got %d", len(c.Words))
	}
	segs := c.Words[1].Segments
	if len(segs) != 2 {
		t.Fatalf("version word: want literal+cmdsub, got %d segments", len(segs))
	}
	if _, ok := segs[0].(Literal); !ok {
		t.Fatalf("segment 0 not literal")
	}
	sub, ok := segs[1].(CmdSub)
	if !ok {
		t.Fatalf("segment 1 not cmdsub")
	}
	inner := commands(sub.Script)
	if len(inner) != 1 || len(inner[0].Words) != 5 {
		t.Fatalf("inner script shape wrong")
	}
	vs, ok := inner[0].Words[2].Segments[0].(VarSub)
	if !ok || vs.Name.Text([]byte(src)) != "github.version" {
		t.Fatalf("inner varsub wrong: %+v", inner[0].Words[2])
	}
}

func TestCommentsOnlyAtCommandPosition(t *testing.T) {
	src := "# real comment\nset x 1 ;# trailing comment\nputs a#b\n"
	s, errs := parseVerified(t, src)
	mustNoErrors(t, errs)
	var nComments, nCommands int
	for _, it := range s.Items {
		switch it.(type) {
		case Comment:
			nComments++
		case Command:
			nCommands++
		}
	}
	if nComments != 2 || nCommands != 2 {
		t.Fatalf("want 2 comments and 2 commands, got %d and %d", nComments, nCommands)
	}
	// a#b is one word of the last command, not a comment
	last := commands(s)[1]
	if got := last.Words[1].Span.Text([]byte(src)); got != "a#b" {
		t.Fatalf("mid-word hash mishandled: %q", got)
	}
}

func TestCommentContinuation(t *testing.T) {
	src := "# comment continues \\\nonto this line\nfoo\n"
	s, errs := parseVerified(t, src)
	mustNoErrors(t, errs)
	if len(s.Items) != 2 {
		t.Fatalf("want comment+command, got %d items", len(s.Items))
	}
	com, ok := s.Items[0].(Comment)
	if !ok {
		t.Fatalf("first item not a comment")
	}
	if got := com.Span.Text([]byte(src)); got != "# comment continues \\\nonto this line" {
		t.Fatalf("comment span = %q", got)
	}
}

func TestSubportScriptLens(t *testing.T) {
	src := "subport libftdi0 {\n    revision            1\n}\n"
	s, errs := parseVerified(t, src)
	mustNoErrors(t, errs)
	c := commands(s)[0]
	braced, ok := c.Words[2].Segments[0].(Braced)
	if !ok {
		t.Fatalf("word 2 not braced")
	}
	inner, lerr := braced.ScriptLens([]byte(src))
	mustNoErrors(t, lerr)
	verifyScript(t, []byte(src), inner, braced.Body.Start, braced.Body.End)
	ic := commands(inner)
	if len(ic) != 1 {
		t.Fatalf("want 1 inner command, got %d", len(ic))
	}
	if name, _ := ic[0].Name([]byte(src)); name != "revision" {
		t.Fatalf("inner command = %q", name)
	}
	if got := ic[0].Words[1].Span.Text([]byte(src)); got != "1" {
		t.Fatalf("revision value = %q", got)
	}
}

func TestExpansionPrefix(t *testing.T) {
	src := "cmd {*}$flags x\n"
	s, errs := parseVerified(t, src)
	mustNoErrors(t, errs)
	w := commands(s)[0].Words[1]
	if !w.Expand {
		t.Fatalf("expansion prefix not recognised")
	}
	if _, ok := w.Segments[0].(VarSub); !ok {
		t.Fatalf("expanded word tail not a varsub")
	}
	// A bare {*} with nothing after it is a braced word, not expansion.
	src2 := "cmd {*}\n"
	s2, errs2 := parseVerified(t, src2)
	mustNoErrors(t, errs2)
	w2 := commands(s2)[0].Words[1]
	if w2.Expand {
		t.Fatalf("bare {*} misread as expansion")
	}
	if _, ok := w2.Segments[0].(Braced); !ok {
		t.Fatalf("bare {*} not a braced word")
	}
}

func TestQuotedWordSubstitutions(t *testing.T) {
	src := "description \"a b [lindex $x 0] c\"\n"
	s, errs := parseVerified(t, src)
	mustNoErrors(t, errs)
	q, ok := commands(s)[0].Words[1].Segments[0].(Quoted)
	if !ok {
		t.Fatalf("word 1 not quoted")
	}
	if len(q.Segments) != 3 {
		t.Fatalf("want literal+cmdsub+literal, got %d", len(q.Segments))
	}
}

func TestArrayIndexWithSpace(t *testing.T) {
	src := "puts $a( b)\n"
	s, errs := parseVerified(t, src)
	mustNoErrors(t, errs)
	c := commands(s)[0]
	if len(c.Words) != 2 {
		t.Fatalf("index with space split the word: %d words", len(c.Words))
	}
	vs, ok := c.Words[1].Segments[0].(VarSub)
	if !ok || !vs.HasIndex {
		t.Fatalf("not an array varsub: %+v", c.Words[1].Segments[0])
	}
	if got := vs.Index.Text([]byte(src)); got != " b" {
		t.Fatalf("index = %q", got)
	}
}

func TestBareDollar(t *testing.T) {
	src := "puts $ x\n"
	s, errs := parseVerified(t, src)
	mustNoErrors(t, errs)
	c := commands(s)[0]
	if len(c.Words) != 3 {
		t.Fatalf("want 3 words, got %d", len(c.Words))
	}
	if _, ok := c.Words[1].Segments[0].(Literal); !ok {
		t.Fatalf("bare dollar not literal")
	}
}

func TestBackslashQuotedBraces(t *testing.T) {
	src := "set x {a \\{ b}\n"
	s, errs := parseVerified(t, src)
	mustNoErrors(t, errs)
	braced, ok := commands(s)[0].Words[2].Segments[0].(Braced)
	if !ok {
		t.Fatalf("word 2 not braced")
	}
	if got := braced.Body.Text([]byte(src)); got != "a \\{ b" {
		t.Fatalf("body = %q", got)
	}
}

func TestMidWordBraceIsOrdinary(t *testing.T) {
	src := "set x a{b}c\n"
	s, errs := parseVerified(t, src)
	mustNoErrors(t, errs)
	w := commands(s)[0].Words[2]
	if len(w.Segments) != 1 {
		t.Fatalf("mid-word brace split segments: %d", len(w.Segments))
	}
	if got := w.Span.Text([]byte(src)); got != "a{b}c" {
		t.Fatalf("word = %q", got)
	}
}

func TestExtraAfterCloseBrace(t *testing.T) {
	src := "set x {a}b\n"
	s, errs := parseVerified(t, src)
	if len(errs) != 1 {
		t.Fatalf("want 1 error, got %v", errs)
	}
	w := commands(s)[0].Words[2]
	if got := w.Span.Text([]byte(src)); got != "{a}b" {
		t.Fatalf("word span = %q", got)
	}
}

func TestUnterminatedBrace(t *testing.T) {
	src := "subport foo {\n    revision 1\n"
	s, errs := parseVerified(t, src)
	if len(errs) == 0 {
		t.Fatalf("unterminated brace produced no error")
	}
	_ = s
}

func TestSemicolonThenComment(t *testing.T) {
	src := "foo; # bar\n"
	s, errs := parseVerified(t, src)
	mustNoErrors(t, errs)
	if len(s.Items) != 2 {
		t.Fatalf("want command+comment, got %d items", len(s.Items))
	}
}

func TestNamespaceVariable(t *testing.T) {
	src := "puts $::tcl_platform(os)\n"
	s, errs := parseVerified(t, src)
	mustNoErrors(t, errs)
	vs, ok := commands(s)[0].Words[1].Segments[0].(VarSub)
	if !ok {
		t.Fatalf("not a varsub")
	}
	if got := vs.Name.Text([]byte(src)); got != "::tcl_platform" {
		t.Fatalf("name = %q", got)
	}
	if !vs.HasIndex || vs.Index.Text([]byte(src)) != "os" {
		t.Fatalf("index wrong")
	}
}

func TestListLensBackslashNewline(t *testing.T) {
	// Rule [9]'s prepass turns backslash-newline (plus following
	// whitespace) into a single space even inside braces, so in a brace
	// body read as a list it separates elements and never forms one. The
	// differential harness caught the original version of this bug.
	src := "checksums {rmd160  abc \\\n    sha256  def}\n"
	s, errs := parseVerified(t, src)
	mustNoErrors(t, errs)
	braced, ok := commands(s)[0].Words[1].Segments[0].(Braced)
	if !ok {
		t.Fatal("word 1 not braced")
	}
	elems, lerr := braced.ListLens([]byte(src))
	mustNoErrors(t, lerr)
	var got []string
	for _, e := range elems {
		got = append(got, e.Text([]byte(src)))
	}
	want := []string{"rmd160", "abc", "sha256", "def"}
	if len(got) != len(want) {
		t.Fatalf("elements = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("elem %d = %q, want %q", i, got[i], want[i])
		}
	}
}
