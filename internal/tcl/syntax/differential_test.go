package syntax

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tcl/rpc"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
	"github.com/herbygillot/dockhand/internal/testenv"
)

// The differential harness: the parser's verdicts cross-checked against a
// real Tcl interpreter — the oracle-is-never-dockhand principle applied to
// the parser itself. Three properties over every fixture:
//
//  1. Completeness agreement: every fixture parses here with zero errors,
//     so Tcl's `info complete` must agree that each is a complete script.
//  2. Truncation: cutting a fixture just before a braced word's close brace
//     must make `info complete` report incomplete — our span for the brace
//     is where Tcl's own balance tracking says it is.
//  3. List agreement: for every brace body our list lens splits cleanly,
//     Tcl's `llength` must see the same element count, and each raw element
//     span must denote exactly the value Tcl extracts with `lindex`.
//
// Property 3 inlines the braced word into an evaluated script — `llength
// {body}` — rather than framing the raw bytes, deliberately: a Portfile's
// brace body always reaches list operations through Tcl's command parser,
// whose rule [9] prepass rewrites backslash-newline even inside braces
// (rule [6]). Framing raw bytes would test the bare list parser, a layer no
// real brace body ever meets alone, and the two differ exactly on
// continuation lines. Properties 1 and 2 frame raw bytes, which is correct
// for them: `info complete` is a whole-source question.

const diffOps = `
proc iscomplete {s} { info complete $s }
::tclrpc::register iscomplete iscomplete
`

func startDiffSession(t *testing.T) *rpc.Session {
	t.Helper()
	path := testenv.Tclsh(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p, err := shell.Start(ctx, path)
	require.NoError(t, err)
	s, err := rpc.New(ctx, p, rpc.WithInit(diffOps))
	require.NoError(t, err) // New kills the proc on failure
	t.Cleanup(func() { s.Close() })
	return s
}

func fixtureFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, root := range fixtureRoots {
		entries, err := os.ReadDir(root)
		require.NoError(t, err)
		for _, e := range entries {
			files = append(files, filepath.Join(root, e.Name()))
		}
	}
	return files
}

func tcl(t *testing.T, s *rpc.Session, op string, args ...string) string {
	t.Helper()
	got, err := s.Call(context.Background(), op, args...)
	require.NoError(t, err, op)
	return got
}

// bracedWords collects every Braced segment in the tree, lens-recursively.
func bracedWords(src []byte, s *Script) []Braced {
	var out []Braced
	var fromSegments func(segs []Segment)
	var fromScript func(sc *Script)
	fromSegments = func(segs []Segment) {
		for _, seg := range segs {
			switch seg := seg.(type) {
			case Braced:
				out = append(out, seg)
			case Quoted:
				fromSegments(seg.Segments)
			case CmdSub:
				fromScript(seg.Script)
			case Literal, VarSub:
				// No nested segments to descend into.
			}
		}
	}
	fromScript = func(sc *Script) {
		for _, it := range sc.Items {
			if c, ok := it.(Command); ok {
				for _, w := range c.Words {
					fromSegments(w.Segments)
				}
			}
		}
	}
	fromScript(s)
	return out
}

func TestDifferentialAgainstTclsh(t *testing.T) {
	s := startDiffSession(t)

	const (
		maxTruncationsPerFile = 3
		maxListsPerFile       = 5
		maxElemsPerList       = 5
	)

	var completeness, truncations, listChecks, elemChecks int
	for _, path := range fixtureFiles(t) {
		src, err := os.ReadFile(path)
		require.NoError(t, err)
		tree, errs := Parse(src)
		require.Empty(t, errs, "%s: fixture no longer parses clean", path)

		probe := string(src)
		if endsWithContinuation(src) {
			// A backslash-newline at EOF splits the oracles: `source`
			// accepts the file (the continuation becomes a space and the
			// command runs), while `info complete` reports that more input
			// could come. The parser sides with source semantics, so the
			// harness supplies the empty continuation line before asking.
			probe += "\n"
		}
		assert.Equal(t, "1", tcl(t, s, "iscomplete", probe),
			"%s: parsed clean here but info complete disagrees", path)
		completeness++

		braced := bracedWords(src, tree)
		for i, b := range braced {
			if i >= maxTruncationsPerFile {
				break
			}
			cut := string(src[:b.Span.End-1])
			assert.Equal(t, "0", tcl(t, s, "iscomplete", cut),
				"%s: truncation inside brace at %d still complete", path, b.Span.End-1)
			truncations++
		}

		lists := 0
		for _, b := range braced {
			if lists >= maxListsPerFile {
				break
			}
			elems, lerr := b.ListLens(src)
			if len(lerr) != 0 {
				continue
			}
			braceWord := b.Span.Text(src)
			got, err := s.Call(context.Background(), "eval", "llength "+braceWord)
			if err != nil {
				// We split it cleanly; Tcl refuses it as a list. A real
				// disagreement, not a harness failure.
				t.Errorf("%s: list at %d: we split %d elements, tcl rejects: %v",
					path, b.Span.Start, len(elems), err)
				lists++
				continue
			}
			assert.Equal(t, strconv.Itoa(len(elems)), got,
				"%s: list at %d: element count disagrees", path, b.Span.Start)
			listChecks++
			for i, e := range elems {
				if i >= maxElemsPerList {
					break
				}
				raw := e.Text(src)
				if !braceBalanced(raw) {
					continue
				}
				want := tcl(t, s, "eval", "lindex "+braceWord+" "+strconv.Itoa(i))
				got := tcl(t, s, "eval", "lindex {"+raw+"} 0")
				assert.Equal(t, want, got,
					"%s: list at %d elem %d: span value disagrees", path, b.Span.Start, i)
				elemChecks++
			}
			lists++
		}
	}
	t.Logf("differential: %d completeness, %d truncation, %d list, %d element checks",
		completeness, truncations, listChecks, elemChecks)
}

// endsWithContinuation reports whether src ends with an unescaped
// backslash-newline — a rule [9] continuation pending at end of input.
func endsWithContinuation(src []byte) bool {
	if len(src) < 2 || src[len(src)-1] != '\n' {
		return false
	}
	n := 0
	for i := len(src) - 2; i >= 0 && src[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

// braceBalanced reports whether raw can be safely inlined between braces:
// its unescaped braces balance and never go negative.
func braceBalanced(raw string) bool {
	depth := 0
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}
