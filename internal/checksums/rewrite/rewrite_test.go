package rewrite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

// parse is the source and its tree, which travel together.
func parse(t *testing.T, s string) ([]byte, *syntax.Script) {
	t.Helper()
	src := []byte(s)
	cst, errs := syntax.Parse(src)
	require.Empty(t, errs)
	return src, cst
}

// topLevel descends into nothing: only top-level commands are in scope.
func topLevel(syntax.Command) bool { return false }

func TestEdits(t *testing.T) {
	src, cst := parse(t, `PortSystem 1.0
checksums           rmd160  aaaa \
                    sha256  bbbb \
                    size    9
`)
	edits, unlocated := Edits(src, cst, topLevel, []checksums.Replacement{
		{Old: "aaaa", New: "cccc", Reason: "checksum rmd160"},
		{Old: "bbbb", New: "dddd", Reason: "checksum sha256"},
		{Old: "9", New: "12", Reason: "checksum size"},
	})
	require.Empty(t, unlocated)
	require.Len(t, edits, 3)
	assert.Equal(t, "cccc", edits[0].New)
	assert.Equal(t, "checksum rmd160", edits[0].Reason)
	// Spans point at the literals themselves, not the whole command.
	assert.Equal(t, "aaaa", string(src[edits[0].Start:edits[0].End]))
	assert.Equal(t, "9", string(src[edits[2].Start:edits[2].End]))
}

func TestEditsChecksumsAppend(t *testing.T) {
	src, cst := parse(t, "checksums sha256 aaaa\nchecksums-append size 9\n")
	edits, unlocated := Edits(src, cst, topLevel, []checksums.Replacement{
		{Old: "aaaa", New: "bbbb"},
		{Old: "9", New: "12"},
	})
	require.Empty(t, unlocated)
	assert.Len(t, edits, 2)
}

func TestEditsReportsUnlocated(t *testing.T) {
	src, cst := parse(t, "checksums sha256 aaaa\n")
	edits, unlocated := Edits(src, cst, topLevel, []checksums.Replacement{
		{Old: "aaaa", New: "bbbb", Reason: "checksum sha256"},
		{Old: "nowhere", New: "x", Reason: "checksum size"},
	})
	assert.Len(t, edits, 1)
	require.Len(t, unlocated, 1)
	assert.Equal(t, "nowhere", unlocated[0].Old)
}

func TestEditsIgnoresOtherCommands(t *testing.T) {
	// The same literal outside a checksums command is not a checksum.
	src, cst := parse(t, "version aaaa\nchecksums sha256 aaaa\n")
	edits, unlocated := Edits(src, cst, topLevel, []checksums.Replacement{{Old: "aaaa", New: "bbbb"}})
	require.Empty(t, unlocated)
	require.Len(t, edits, 1)
	assert.Greater(t, edits[0].Start, 12, "the version line's literal must be left alone")
}

func TestEditsDescendsWhereScopeSays(t *testing.T) {
	src, cst := parse(t, "if {1} {\n    checksums sha256 aaaa\n}\n")
	// Out of scope: nothing found.
	_, unlocated := Edits(src, cst, topLevel, []checksums.Replacement{{Old: "aaaa", New: "bbbb"}})
	assert.Len(t, unlocated, 1)

	// In scope: the caller's predicate opens the branch.
	intoIf := func(cmd syntax.Command) bool {
		name, ok := cmd.Name(src)
		return ok && name == "if"
	}
	edits, unlocated := Edits(src, cst, intoIf, []checksums.Replacement{{Old: "aaaa", New: "bbbb"}})
	require.Empty(t, unlocated)
	assert.Len(t, edits, 1)
}

func TestEditsFirstUnclaimedMatchWins(t *testing.T) {
	// A value repeated across two files claims each occurrence once.
	src, cst := parse(t, "checksums a.tar.gz sha256 same b.tar.gz sha256 same\n")
	edits, unlocated := Edits(src, cst, topLevel, []checksums.Replacement{
		{Old: "same", New: "first"},
		{Old: "same", New: "second"},
	})
	require.Empty(t, unlocated)
	require.Len(t, edits, 2)
	assert.Equal(t, "first", edits[0].New)
	assert.Equal(t, "second", edits[1].New)
	assert.Less(t, edits[0].Start, edits[1].Start)
}

// A value that is already what it should be is located, so the caller
// hears no complaint about it, but yields no edit: a plan must not list
// a change that changes nothing.
func TestEditsSkipReplacementsThatChangeNothing(t *testing.T) {
	src, cst := parse(t, "checksums rmd160 aaaa sha256 bbbb size 9\n")
	edits, unlocated := Edits(src, cst, topLevel, []checksums.Replacement{
		{Old: "aaaa", New: "aaaa", Reason: "checksum rmd160"},
		{Old: "bbbb", New: "cccc", Reason: "checksum sha256"},
	})
	assert.Empty(t, unlocated, "an unchanged value is still located")
	require.Len(t, edits, 1)
	assert.Equal(t, "cccc", edits[0].New)
}
