// Package patch reads a unified diff closely enough to move its hunks
// and no further. A port's patchfile was written against one release;
// the next release shifts the lines it touches, and the hunks still
// apply — patch(1) finds them at an offset — but the @@ headers now
// say the wrong numbers. Refreshing means rewriting those numbers and
// nothing else, so that the patch the maintainer commits is the patch
// they wrote, at the lines the new source actually has.
//
// The ruling this package implements (2026-09-04) is the simplest form
// of that: for each hunk, find where its before-block — every context
// and removed line, in order, verbatim — occurs in the new file, and
// if it occurs exactly once, move the hunk there. Anything else gives
// up on the whole patch. No fuzz, no partial application, no
// whitespace tolerance. That is why the parser keeps every byte of its
// input: a hunk body is never interpreted, only copied back out, and
// the only bytes Relocate may change are the four numbers on an @@
// line. Parse followed by Bytes is the identity, and a test holds it
// there.
package patch

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ErrMalformed reports input this package cannot read as a unified
// diff: no ---/+++ header at all, a header with no hunk under it, a
// hunk whose body does not add up to the counts on its @@ line, a
// truncated hunk. A context-format diff arrives here too, because its
// --- lines are never followed by +++. The wrapped message names the
// input line, and callers give up: a patch that cannot be read is a
// patch that cannot be refreshed, and by the ruling that is a decline,
// not a guess.
var ErrMalformed = errors.New("patch: not a unified diff")

// Patch is a parsed unified diff, kept as the slices of its own bytes.
type Patch struct {
	// Files are the ---/+++ sections in input order.
	Files []File
	// Trailer is everything after the last hunk, verbatim: a blank
	// line, a signature, nothing at all.
	Trailer []byte
}

// File is one ---/+++ section and the hunks under it.
type File struct {
	// Preamble is every byte between the previous hunk (or the start
	// of the input) and this section's --- line: the prose a
	// maintainer put at the top, a "diff -u" command line, git's
	// "diff --git" and "index" lines. It is preserved, never read.
	Preamble []byte
	// Header is the --- and +++ lines, verbatim.
	Header []byte
	// Old and New are the names the --- and +++ lines carry, as
	// written: a name ends at the first tab or space, which is where
	// diff puts the timestamp. Stripping happens in Target, because
	// the -p level is the Portfile's to say and not the patch's.
	Old, New string
	// Hunks are the @@ blocks, in order.
	Hunks []Hunk
}

// Hunk is one @@ block. The four numbers are the ranges its header
// declares; Body is every line under the header, verbatim, including a
// "\ No newline at end of file" marker if the diff carries one.
type Hunk struct {
	OldStart, OldCount int
	NewStart, NewCount int
	Body               []byte

	// tail is the rest of the @@ line after the ranges — the closing
	// " @@", any function name diff appended, and the line ending —
	// and oldBare/newBare record that a side spelled no count, which
	// diff does when the count is one. Both exist so that rendering a
	// hunk whose numbers did not change reproduces its header
	// byte-for-byte.
	tail             []byte
	oldBare, newBare bool
}

// devNull is the name diff gives the absent side of a file created or
// deleted by the patch.
const devNull = "/dev/null"

// Target is the path the section patches, relative to the directory
// patch(1) is run in, after stripping strip leading components as -p
// would. It is the +++ name, because that is the file the patch
// produces and the one that exists in the tree — MacPorts' "foo.orig"
// convention puts a name on the --- line that never exists — unless
// the +++ side is /dev/null, a deletion, in which case only the ---
// name names anything.
//
// Stripping follows patch(1): -pN removes the smallest prefix holding N
// slashes, and a name with fewer slashes than that keeps its last
// component rather than vanishing.
func (f File) Target(strip int) string {
	name := f.New
	if name == devNull {
		name = f.Old
	}
	for ; strip > 0; strip-- {
		i := strings.IndexByte(name, '/')
		if i < 0 {
			break
		}
		name = name[i+1:]
	}
	return name
}

// Parse reads src as a unified diff. It fails with ErrMalformed for
// anything it cannot account for byte by byte, because a parse that
// skipped what it did not understand would let Relocate write a patch
// missing the part it skipped.
//
// Prose is the one thing skipped, and an @@ line is never prose. A
// hunk whose header undercounts its body leaves the surplus lines
// where the next @@ line is looked for, so that hunk and every one
// after it in the section would fall through to here and ride into
// the next preamble or the trailer, unread — and a Relocate over what
// was read would rewrite the hunks before them and hand back a patch
// half refreshed, the exact artifact the ruling forbids. An @@ line
// where a section is not open is therefore the parse giving up.
func Parse(src []byte) (Patch, error) {
	s := scanner{src: src}
	var p Patch
	pre := 0
	for !s.eof() {
		if !s.atHeader() {
			if bytes.HasPrefix(s.peek(), []byte("@@ ")) {
				return Patch{}, fmt.Errorf("%w: @@ line at line %d belongs to no section; a hunk above it has more lines than its counts, or it is prose", ErrMalformed, s.line+1)
			}
			s.next()
			continue
		}
		f := File{Preamble: src[pre:s.pos]}
		start := s.pos
		f.Old = headerName(s.next())
		f.New = headerName(s.next())
		f.Header = src[start:s.pos]
		for bytes.HasPrefix(s.peek(), []byte("@@ ")) {
			h, err := s.hunk()
			if err != nil {
				return Patch{}, err
			}
			f.Hunks = append(f.Hunks, h)
		}
		if len(f.Hunks) == 0 {
			return Patch{}, fmt.Errorf("%w: no hunk under the header at line %d", ErrMalformed, s.line)
		}
		p.Files = append(p.Files, f)
		pre = s.pos
	}
	if len(p.Files) == 0 {
		return Patch{}, fmt.Errorf("%w: no ---/+++ header", ErrMalformed)
	}
	p.Trailer = src[pre:]
	return p, nil
}

// Bytes renders the patch. Over an unmodified Parse result it returns
// the input exactly; after Relocate it differs only on the @@ lines
// whose hunks moved.
func (p Patch) Bytes() []byte {
	var b bytes.Buffer
	for _, f := range p.Files {
		b.Write(f.Preamble)
		b.Write(f.Header)
		for _, h := range f.Hunks {
			b.WriteString("@@ -")
			writeRange(&b, h.OldStart, h.OldCount, h.oldBare)
			b.WriteString(" +")
			writeRange(&b, h.NewStart, h.NewCount, h.newBare)
			b.Write(h.tail)
			b.Write(h.Body)
		}
	}
	b.Write(p.Trailer)
	return b.Bytes()
}

func writeRange(b *bytes.Buffer, start, count int, bare bool) {
	b.WriteString(strconv.Itoa(start))
	if !bare {
		b.WriteByte(',')
		b.WriteString(strconv.Itoa(count))
	}
}

// before returns the hunk's before-block: the content of every context
// and removed line, in order, without its line ending. This is what
// the target file must contain verbatim for the hunk to apply there.
// Only the '\n' is taken off, so a '\r' before it stays part of the
// line and a CRLF file matches only a CRLF patch, which is the ruling's
// "verbatim" and not a normalisation this package is entitled to.
func (h Hunk) before() [][]byte {
	var block [][]byte
	for _, line := range splitLines(h.Body) {
		switch {
		case len(line) == 0:
			// A bare empty line where a body line belongs is an empty
			// context line whose space an editor stripped; patch(1)
			// reads it that way and so does hunk below.
			block = append(block, line)
		case line[0] == ' ', line[0] == '-':
			block = append(block, line[1:])
		}
	}
	return block
}

// hunkHeader matches the ranges of an @@ line and nothing after them.
// The closing " @@" is checked separately so that it, any function
// name diff appended, and the line ending are one slice of the input
// kept as the hunk's tail.
var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(,(\d+))? \+(\d+)(,(\d+))?`)

// scanner walks the input a line at a time, each line delivered with
// its line ending so that the slices it hands out concatenate back to
// the input.
type scanner struct {
	src  []byte
	pos  int
	line int
}

func (s *scanner) eof() bool { return s.pos >= len(s.src) }

// peek returns the next line including its '\n', or the unterminated
// remainder at the end of the input, or nil at end of input.
func (s *scanner) peek() []byte {
	if s.eof() {
		return nil
	}
	rest := s.src[s.pos:]
	if i := bytes.IndexByte(rest, '\n'); i >= 0 {
		return rest[:i+1]
	}
	return rest
}

func (s *scanner) next() []byte {
	l := s.peek()
	s.pos += len(l)
	s.line++
	return l
}

// atHeader reports whether the scanner stands on a file header: a ---
// line with a +++ line under it. Both are required because "--- " is
// also how a context-format diff names its new file, how a removed
// line beginning "-- " renders inside a hunk body, and how prose draws
// a rule; only the pair opens a section. Bodies are consumed by count
// and never reach this check, so the removed-line case is a concern
// for prose alone.
func (s *scanner) atHeader() bool {
	l := s.peek()
	if !bytes.HasPrefix(l, []byte("--- ")) {
		return false
	}
	after := s.src[s.pos+len(l):]
	return bytes.HasPrefix(after, []byte("+++ "))
}

// headerName takes the name off a --- or +++ line. diff separates the
// name from the timestamp with a tab; a hand-written header may use a
// space. Nothing else is trimmed: this package normalises nothing, and
// a name the reader cannot find is the give-up the ruling asks for.
func headerName(line []byte) string {
	name := line[len("--- "):]
	if i := bytes.IndexAny(name, "\t "); i >= 0 {
		name = name[:i]
	}
	return string(bytes.TrimSuffix(name, []byte("\n")))
}

// hunk parses one @@ block, consuming exactly as many body lines as
// the header's counts call for. Counting is the only correct way to
// find the end: a body line may begin with anything the diff's own
// framing begins with, so a scan for the next header would end a hunk
// early at a removed line whose content starts with "-- ".
func (s *scanner) hunk() (Hunk, error) {
	at := s.line + 1
	head := s.next()
	m := hunkHeader.FindSubmatch(head)
	if m == nil || !bytes.HasPrefix(head[len(m[0]):], []byte(" @@")) {
		return Hunk{}, fmt.Errorf("%w: unreadable @@ line at line %d", ErrMalformed, at)
	}
	h := Hunk{
		OldStart: atoi(m[1]),
		OldCount: 1,
		NewStart: atoi(m[4]),
		NewCount: 1,
		oldBare:  m[2] == nil,
		newBare:  m[5] == nil,
		tail:     head[len(m[0]):],
	}
	if !h.oldBare {
		h.OldCount = atoi(m[3])
	}
	if !h.newBare {
		h.NewCount = atoi(m[6])
	}

	start := s.pos
	old, added := h.OldCount, h.NewCount
	for old > 0 || added > 0 {
		l := s.peek()
		if l == nil {
			return Hunk{}, fmt.Errorf("%w: hunk at line %d ends before its counts are met", ErrMalformed, at)
		}
		switch {
		case len(bytes.TrimSuffix(l, []byte("\n"))) == 0:
			// The empty-context-line tolerance before explains.
			old--
			added--
		case l[0] == ' ':
			old--
			added--
		case l[0] == '-':
			old--
		case l[0] == '+':
			added--
		case l[0] == '\\':
			// "\ No newline at end of file" annotates the line above
			// it and counts on neither side.
		default:
			return Hunk{}, fmt.Errorf("%w: unexpected line %d inside the hunk at line %d", ErrMalformed, s.line+1, at)
		}
		if old < 0 || added < 0 {
			return Hunk{}, fmt.Errorf("%w: hunk at line %d has more lines than its counts", ErrMalformed, at)
		}
		s.next()
	}
	// The marker may annotate the hunk's last line, after the counts
	// are already met.
	if bytes.HasPrefix(s.peek(), []byte("\\")) {
		s.next()
	}
	h.Body = s.src[start:s.pos]
	return h, nil
}

// atoi is for digit runs the header regexp has already matched.
func atoi(b []byte) int {
	n, _ := strconv.Atoi(string(b))
	return n
}

// splitLines returns src's lines without their '\n'. A trailing '\n'
// ends the last line rather than starting an empty one; an
// unterminated last line is still a line. Empty input has no lines.
func splitLines(src []byte) [][]byte {
	if len(src) == 0 {
		return nil
	}
	lines := bytes.Split(src, []byte("\n"))
	if len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
}
