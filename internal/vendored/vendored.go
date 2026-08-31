// Package vendored draws the boundary around a Portfile's vendored
// dependency blocks — cargo.crates, go.vendors — without reading inside
// them. A block is opaque (D6): dockhand never interprets an entry, and
// regenerates the whole block from the tool that owns its format.
//
// What lives here is what every vendored block has in common: finding
// the command that carries one, telling the distfiles it supplied from
// the port's own, checking that a generator produced a block at all,
// and turning a regenerated block into an edit. What a particular
// block's entries look like, and which tool writes them, belongs to a
// subpackage named for that tool — see vendored/cargo2port.
//
// The boundary has to be found in two senses. In the source a block is
// one command, whose span is the replacement target. In an evaluated
// context a block is invisible: its entries have already expanded into
// distfiles and checksums indistinguishable from the port's own. A
// cargo port evaluates to 205 distfiles of which one is the source
// tarball, and 619 checksum tokens of which seven were written by hand.
//
// Telling them apart by name would mean reconstructing how a Portfile
// spells its own distfile — ${distname}${extract.suffix}, an explicit
// foo-${version}.tar.gz, a fetch-group tag — which is guesswork that
// fails quietly. Instead the block is asked what it supplies, and what
// survives subtracting that is the port's own. Every supplied name must
// appear among the evaluated distfiles, so a wrong assumption surfaces
// as an error rather than as a wrong answer.
package vendored

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/herbygillot/dockhand/internal/edit"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

// Kind is a vendored block's flavor. Its string is both the option the
// block is read from and the command that sets it.
type Kind int

const (
	// CargoCrates is the Rust block, regenerated from a Cargo.lock.
	CargoCrates Kind = iota
	// GoVendors is the Go block, regenerated from a module's source.
	GoVendors
)

func (k Kind) String() string {
	switch k {
	case CargoCrates:
		return "cargo.crates"
	case GoVendors:
		return "go.vendors"
	}
	return "unknown"
}

var (
	// ErrNoBlock reports that the source sets no block of the kind asked for.
	ErrNoBlock = errors.New("vendored: no block found")
	// ErrMultipleBlocks reports that more than one command contributes to
	// the block. No port in the tree does this today; replacing one
	// command and leaving another would double-count, so it is refused
	// rather than guessed at.
	ErrMultipleBlocks = errors.New("vendored: block set by more than one command")
	// ErrMalformed reports a block whose evaluated option is not the shape
	// its PortGroup reads.
	ErrMalformed = errors.New("vendored: malformed block")
	// ErrUnaccounted reports that a block supplies a distfile the
	// evaluated context does not have — the construction a generator's
	// package assumes has moved, and subtraction can no longer be trusted.
	ErrUnaccounted = errors.New("vendored: block supplies a distfile the port does not have")
	// ErrNoGenerator reports that the tool owning a block's format is not
	// installed. It is a fact about the machine, which doctor probes for,
	// never a finding about a port.
	ErrNoGenerator = errors.New("vendored: block generator not found")
	// ErrEmptyBlock reports that a generator produced no block. Both
	// generators can fail this way while exiting zero — cargo2port says
	// so on stdout, go2port simply omits the block — so an exit status
	// alone would accept silence as success and write a Portfile that
	// vendors nothing.
	ErrEmptyBlock = errors.New("vendored: generator produced no block")
)

// Own returns the distfiles a context declares for itself: the evaluated
// distfiles less those a vendored block supplied, in evaluation order
// and with the :tag fetch-group suffixes stripped, since checksums and
// the fetch surface speak bare names.
//
// A supplied name that is not among the distfiles is an error, not an
// omission. Subtraction is only sound while a generator package's idea
// of what its block contributes matches the PortGroup's, and that
// mismatch is exactly what a leftover proves.
func Own(distfiles, supplied []string) ([]string, error) {
	remaining := make(map[string]int, len(supplied))
	for _, s := range supplied {
		remaining[s]++
	}
	own := make([]string, 0, max(len(distfiles)-len(supplied), 0))
	for _, d := range distfiles {
		name, _, _ := strings.Cut(d, ":")
		if remaining[name] > 0 {
			remaining[name]--
			continue
		}
		own = append(own, name)
	}
	var leftover []string
	for name, n := range remaining {
		if n > 0 {
			leftover = append(leftover, name)
		}
	}
	if len(leftover) != 0 {
		sort.Strings(leftover)
		return nil, fmt.Errorf("%w: %s (%d in all)", ErrUnaccounted, leftover[0], len(leftover))
	}
	return own, nil
}

// Locate finds the span of the command carrying a block, which is the
// whole command including its line continuations — the replacement
// target for a regenerated block.
//
// Contributing -append forms are counted too: none exist in the tree
// today, and replacing one command while leaving another would produce
// a Portfile stating its dependencies twice.
//
// scope is injected rather than assumed, as in checksums/rewrite:
// callers pass portstyle.ScopeOf for the context being edited.
func Locate(src []byte, cst *syntax.Script, scope func(syntax.Command) bool, k Kind) (text.Span, error) {
	block, appended := k.String(), k.String()+"-append"
	var found text.Span
	n := 0
	for cmd := range cst.Commands(src, scope) {
		name, ok := cmd.Name(src)
		if !ok || (name != block && name != appended) {
			continue
		}
		n++
		found = cmd.Span
	}
	switch n {
	case 0:
		return text.Span{}, fmt.Errorf("%w: %s", ErrNoBlock, k)
	case 1:
		return found, nil
	}
	return text.Span{}, fmt.Errorf("%w: %s set %d times", ErrMultipleBlocks, k, n)
}

// ValidateBlock accepts a generator's output only if it is the block it
// claims to be, and returns it trimmed of the trailing newline a located
// span does not include.
//
// This is where the generators' shared failure mode is caught: producing
// nothing while reporting success. A caller that only checked the exit
// status would write an empty block over a real one.
func ValidateBlock(out []byte, k Kind) ([]byte, error) {
	block := bytes.TrimRight(out, "\n")
	if !bytes.HasPrefix(block, []byte(k.String())) {
		first, _, _ := bytes.Cut(block, []byte("\n"))
		return nil, fmt.Errorf("%w: %s: got %q", ErrEmptyBlock, k, strings.TrimSpace(string(first)))
	}
	return block, nil
}

// Edit replaces a located block with a regenerated one. The block is
// taken verbatim: reflowing it to preserve a port's existing column
// widths would mean interpreting what this package has no business
// reading, and the generator's own output is what a maintainer running
// it by hand would commit.
func Edit(src []byte, span text.Span, block []byte, k Kind) edit.Edit {
	return edit.Edit{
		Start:  span.Start,
		End:    span.End,
		Old:    span.Text(src),
		New:    string(block),
		Reason: "regenerate " + k.String(),
	}
}
