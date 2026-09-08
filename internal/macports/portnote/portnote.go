// Package portnote reads what a Portfile says about itself in its own
// comments.
//
// A comment is the one place a maintainer writes down something no
// field of the file can hold: that every dependent has to be
// revision-bumped when this port moves, that the version is held where
// it is and must not be advanced. Three parts of dockhand read those
// annotations for three different purposes — the planner quotes one
// into a finding a person answers, the sweep excludes a port whose
// comments oblige a cascade it cannot perform, and the roster that
// fills a cohort proposal asks whether a full pass over the PortIndex
// is worth paying for — and each of them used to carry its own copy of
// the reader.
//
// Nothing here decides anything, and that is the whole of why one
// reader can serve three callers with three different purposes. This
// package recognises the shapes the tree writes and hands back what it
// read: the comment block verbatim, the ports a sentence named, whether
// the form was the one that names nobody on purpose. Whether that is a
// proposal, an exclusion or a reason to read an index is the caller's,
// and none of those words appears below.
//
// The shapes are transcribed from the real tree rather than invented,
// and the invented sentence is worth naming because it was a plan's
// own: "increase the revision of the following ports when updating"
// matches ZERO Portfiles. What is there is a family, and the tests
// carry it port by port with the Portfile each row was read from.
package portnote

import "strings"

// Block is one run of adjacent comment lines: the bytes as the Portfile
// writes them, the same lines as prose for a pattern to read, and where
// the run sits in the file.
//
// The three readings are kept apart because they are wanted apart. A
// quote must be verbatim or it is not a quote; a sentence that wrapped
// across two comment lines is one sentence to a pattern and two lines
// to a roster written one item per line; and a rule that asks what a
// comment ABUTS needs the line numbers, which neither of the other two
// preserves.
type Block struct {
	// Text is verbatim — the '#' and the indentation as they stand,
	// lines joined by the newline that separated them.
	Text string
	// Prose is the same lines with the comment marker and any "NOTE:"
	// stripped, joined by one space, so a sentence that wrapped reads as
	// one sentence.
	Prose string
	// Lines are the prose of each line on its own, for the roster forms
	// that are written one item per line.
	Lines []string
	// Start and End are the indices of the block's first and last lines
	// in the file, counting every line and from zero, for a rule that
	// asks what the block sits against.
	Start int
	End   int
}

// Blocks splits a Portfile into its runs of adjacent comment lines.
//
// Every comment line counts, wherever it sits. The rule that cares
// where a comment is — the rider's first proof — is about EDITING
// bytes; this only reads them, and privoxy's own revbump instruction
// lives inside a subport block, so a reader that skipped the braced
// bodies would miss shapes the tree actually writes.
//
// A block ends at the first line that is not a comment, which is what
// keeps a negation further down a file from silencing an instruction
// above it: they are two blocks and every rule here reads one block at
// a time.
func Blocks(src []byte) []Block {
	lines := strings.Split(string(src), "\n")
	var out []Block
	var cur Block
	open := false
	flush := func(end int) {
		if open {
			cur.End = end
			out = append(out, cur)
		}
		cur, open = Block{}, false
	}
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if !strings.HasPrefix(trimmed, "#") {
			flush(i - 1)
			continue
		}
		line := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "NOTE:"))
		if !open {
			open, cur.Start, cur.Text = true, i, raw
		} else {
			cur.Text += "\n" + raw
		}
		cur.Prose = strings.TrimSpace(cur.Prose + " " + line)
		cur.Lines = append(cur.Lines, line)
	}
	flush(len(lines) - 1)
	return out
}
