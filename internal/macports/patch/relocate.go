package patch

import (
	"bytes"
	"fmt"
	"slices"
)

// Reason says why a hunk did not relocate. The set is closed and each
// member is a way the ruling says to give up: the before-block is not
// there, it is there more than once, the file it should be in could
// not be produced, or where it landed depends on another hunk.
type Reason int

const (
	// NotFound is a before-block that occurs nowhere in the target.
	// The lines the hunk expected to change have themselves changed,
	// and that is a refresh only a person can make.
	NotFound Reason = iota
	// Ambiguous is a before-block that occurs more than once. Either
	// place might be the one the patch meant, and the ruling is to
	// give up rather than choose; patch(1) would take the nearest,
	// which is a guess this package does not make. A hunk with no
	// before-block at all — a zero-context insertion — is ambiguous
	// by construction, since nothing anchors it anywhere.
	Ambiguous
	// Unreadable is a target the reader could not supply: the file is
	// not in the distfile under the name the header gives, or the
	// archive could not be read. The wrapped error says which.
	Unreadable
	// Entangled is a hunk whose place depends on an earlier one. Its
	// before-block was found, once, but it lands on or before the hunk
	// ahead of it in the same section, or its section patches a file
	// an earlier section already patched, so its numbers are counted
	// from a file this package never sees. Rewriting the header would
	// produce a diff no diff would, and the ruling says give up.
	Entangled
)

func (r Reason) String() string {
	switch r {
	case NotFound:
		return "its before-block occurs nowhere in the file"
	case Ambiguous:
		return "its before-block occurs more than once in the file"
	case Unreadable:
		return "the file could not be read"
	case Entangled:
		return "its place depends on an earlier hunk"
	}
	return "unknown reason"
}

// RelocateError names the hunk that stopped a relocation and why. It
// is typed rather than a sentinel because the caller's decline has to
// say which file and which hunk, and those are the answer, not the
// sentence: a planner that had to parse them back out of the message
// could not be trusted to.
type RelocateError struct {
	// File is the target path as looked for, after stripping.
	File string
	// Hunk numbers the hunk within its section as patch(1) does, from
	// one, so that "hunk #2" here is the same hunk patch would report.
	Hunk   int
	Reason Reason
	// Err is the reader's error when Reason is Unreadable, nil
	// otherwise.
	Err error
}

func (e *RelocateError) Error() string {
	s := fmt.Sprintf("patch: %s hunk #%d: %s", e.File, e.Hunk, e.Reason)
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}

// Unwrap exposes the reader's error, so a caller can tell a member
// missing from the archive from an archive that would not open.
func (e *RelocateError) Unwrap() error { return e.Err }

// Result is a relocation that succeeded for every hunk.
type Result struct {
	// Bytes is the refreshed patch: the input, with the numbers on the
	// @@ lines of the hunks that moved rewritten and nothing else
	// touched.
	Bytes []byte
	// Hunks records where every hunk landed, moved or not, in patch
	// order.
	Hunks []Placement
}

// Placement is one hunk's old and new position on the file's old side.
// The new side moved by the same amount; it is not reported because
// it is not a fact about the file, only about the hunks before it.
type Placement struct {
	// File is the target path after stripping.
	File string
	// Hunk numbers the hunk within its section from one, as
	// RelocateError does.
	Hunk     int
	OldStart int
	NewStart int
}

// Moved reports whether the hunk's position changed.
func (p Placement) Moved() bool { return p.OldStart != p.NewStart }

// Moved counts the hunks whose position changed. Zero means the
// refreshed bytes are the input and there is nothing to write.
func (r Result) Moved() int {
	n := 0
	for _, p := range r.Hunks {
		if p.Moved() {
			n++
		}
	}
	return n
}

// Relocate finds every hunk's before-block in the file the reader
// supplies for its section and rewrites the @@ numbers to say where it
// was found. The reader is handed each section's Target after strip
// components are removed — strip is the Portfile's -p level — and is
// called once per section, on its first hunk that needs the file; a
// section that only creates a file never asks for it. The receiver is
// not modified.
//
// The first hunk that does not relocate stops the whole patch with a
// *RelocateError: there is no partial result, because a patch with
// one hunk refreshed and one not is a patch that does not apply.
func (p Patch) Relocate(read func(path string) ([]byte, error), strip int) (Result, error) {
	out := Patch{Files: make([]File, len(p.Files)), Trailer: p.Trailer}
	var placed []Placement
	seen := map[string]bool{}
	for i, f := range p.Files {
		target := f.Target(strip)
		out.Files[i] = f
		out.Files[i].Hunks = slices.Clone(f.Hunks)

		// A file patched by two sections has its second section's
		// numbers counted from the file after the first, which the
		// reader cannot supply. Entangled, on the section's first hunk.
		if seen[target] {
			return Result{}, &RelocateError{File: target, Hunk: 1, Reason: Entangled}
		}
		seen[target] = true

		var lines [][]byte
		loaded := false
		// prevEnd is the last old-side line the previous hunk covers
		// after relocation; the next hunk must start past it.
		prevEnd := 0
		for j := range out.Files[i].Hunks {
			h := &out.Files[i].Hunks[j]
			n := j + 1
			block := h.before()
			if len(block) == 0 {
				// Nothing anchors a hunk with no before-block. A file
				// creation (-0,0) has nowhere else to be and stays;
				// any other zero-context hunk could be anywhere.
				if h.OldStart == 0 && h.OldCount == 0 {
					placed = append(placed, Placement{File: target, Hunk: n, OldStart: 0, NewStart: 0})
					continue
				}
				return Result{}, &RelocateError{File: target, Hunk: n, Reason: Ambiguous}
			}
			if !loaded {
				src, err := read(target)
				if err != nil {
					return Result{}, &RelocateError{File: target, Hunk: n, Reason: Unreadable, Err: err}
				}
				lines = splitLines(src)
				loaded = true
			}
			first, count := occurrences(lines, block)
			switch {
			case count == 0:
				return Result{}, &RelocateError{File: target, Hunk: n, Reason: NotFound}
			case count > 1:
				return Result{}, &RelocateError{File: target, Hunk: n, Reason: Ambiguous}
			}
			at := first + 1
			if at <= prevEnd {
				return Result{}, &RelocateError{File: target, Hunk: n, Reason: Entangled}
			}
			prevEnd = at + h.OldCount - 1

			// The new side moves by the same distance. Its start is the
			// old start plus the net lines the hunks before it added,
			// and neither of those changed: bodies are never touched.
			d := at - h.OldStart
			placed = append(placed, Placement{File: target, Hunk: n, OldStart: h.OldStart, NewStart: at})
			h.OldStart = at
			h.NewStart += d
		}
	}
	return Result{Bytes: out.Bytes(), Hunks: placed}, nil
}

// occurrences finds where block occurs in lines as a run of whole
// lines, verbatim. It returns the first position and how many there
// are, stopping at two: the caller only needs to know that the block
// is there once, or not once.
func occurrences(lines, block [][]byte) (first, count int) {
	for i := 0; i+len(block) <= len(lines); i++ {
		if !matchAt(lines, block, i) {
			continue
		}
		if count == 0 {
			first = i
		}
		count++
		if count > 1 {
			break
		}
	}
	return first, count
}

func matchAt(lines, block [][]byte, at int) bool {
	for k, want := range block {
		if !bytes.Equal(lines[at+k], want) {
			return false
		}
	}
	return true
}
