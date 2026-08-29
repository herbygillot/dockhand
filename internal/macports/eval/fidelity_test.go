package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/tcl/syntax"
	"github.com/herbygillot/dockhand/internal/text"
)

// The fidelity loop, end to end: snapshot, locate a value's span in the
// source, edit by byte-span replacement, re-snapshot, and require the
// observed Delta to equal the predicted one exactly. This is D2's central
// asymmetry executing — values from evaluation, locations from syntax,
// edits as bytes, the oracle judging the result — and D13's totality
// underneath it: the comparison sees every context.

// locate returns the span of the named command's nth word, corroborated:
// the span's text must equal the value evaluation reported, or the
// location is wrong and the test stops before editing anything.
func locate(t *testing.T, src []byte, command string, word int, evaluated string) text.Span {
	t.Helper()
	tree, errs := syntax.Parse(src)
	require.Empty(t, errs)
	for _, it := range tree.Items {
		cmd, ok := it.(syntax.Command)
		if !ok {
			continue
		}
		if name, ok := cmd.Name(src); !ok || name != command {
			continue
		}
		require.Greater(t, len(cmd.Words), word)
		span := cmd.Words[word].Span
		require.Equal(t, evaluated, span.Text(src),
			"located span disagrees with evaluation; refusing to edit")
		return span
	}
	t.Fatalf("no command %q found", command)
	return text.Span{}
}

func rewrite(t *testing.T, dir string, edits []text.Edit) {
	t.Helper()
	path := filepath.Join(dir, "Portfile")
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	out, err := text.Apply(src, edits)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, out, 0o644))
}

// A bump on a literal version line: the predicted delta — version moved,
// and the derived default distfiles moved with it — must equal the
// observed one, field for field, with nothing else disturbed.
func TestFidelityLiteralBump(t *testing.T) {
	e := newEvaluator(t)
	dir := portdirWith(t, `PortSystem 1.0
name                fidelity
version             1.2.3
revision            7
categories          devel
license             MIT
`)
	before, err := e.Snapshot(context.Background(), dir, "")
	require.NoError(t, err)
	kk := key("fidelity")
	require.Equal(t, "1.2.3", before[kk].Version)

	src, err := os.ReadFile(filepath.Join(dir, "Portfile"))
	require.NoError(t, err)
	span := locate(t, src, "version", 1, "1.2.3")
	rewrite(t, dir, []text.Edit{{Span: span, New: []byte("9.9.9")}})

	after, err := e.Snapshot(context.Background(), dir, "")
	require.NoError(t, err)

	predicted := info.Delta{Changed: map[info.SubportKey][]info.FieldChange{
		kk: {
			{Field: info.FieldVersion, Old: []string{"1.2.3"}, New: []string{"9.9.9"}},
			{Field: info.FieldDistfiles,
				Old: []string{"fidelity-1.2.3.tar.gz"},
				New: []string{"fidelity-9.9.9.tar.gz"}},
		},
	}}
	observed := before.Diff(after)
	require.True(t, predicted.Equal(observed),
		"predicted %+v\nobserved %+v", predicted, observed)
}

// The same loop through a PortGroup carrier: ivy's version is word 2 of
// go.setup, and the prediction is built the way a planner would build it —
// from the before-snapshot's values plus the intent.
func TestFidelityPortgroupCarrierBump(t *testing.T) {
	e := newEvaluator(t)
	dir := fixturePortdir(t, "math__ivy")
	before, err := e.Snapshot(context.Background(), dir, "")
	require.NoError(t, err)
	kk := key("ivy")
	oldV := before[kk].Version
	require.NotEmpty(t, oldV)
	newV := "0.99.0"

	src, err := os.ReadFile(filepath.Join(dir, "Portfile"))
	require.NoError(t, err)
	span := locate(t, src, "go.setup", 2, oldV)
	rewrite(t, dir, []text.Edit{{Span: span, New: []byte(newV)}})

	after, err := e.Snapshot(context.Background(), dir, "")
	require.NoError(t, err)

	var oldD, newD []string
	for _, d := range before[kk].Distfiles {
		oldD = append(oldD, d)
		newD = append(newD, strings.ReplaceAll(d, oldV, newV))
	}
	predicted := info.Delta{Changed: map[info.SubportKey][]info.FieldChange{
		kk: {
			{Field: info.FieldVersion, Old: []string{oldV}, New: []string{newV}},
			{Field: info.FieldDistfiles, Old: oldD, New: newD},
		},
	}}
	observed := before.Diff(after)
	require.True(t, predicted.Equal(observed),
		"predicted %+v\nobserved %+v", predicted, observed)
}

// An evaluation-invisible edit — a trailing comment — must produce the
// empty delta: the no-op has a name, and this is it.
func TestFidelityNoOpEdit(t *testing.T) {
	e := newEvaluator(t)
	dir := portdirWith(t, `PortSystem 1.0
name                quiet
version             1.0
categories          devel
`)
	before, err := e.Snapshot(context.Background(), dir, "")
	require.NoError(t, err)

	src, err := os.ReadFile(filepath.Join(dir, "Portfile"))
	require.NoError(t, err)
	end := text.Span{Start: len(src), End: len(src)}
	rewrite(t, dir, []text.Edit{{Span: end, New: []byte("# fidelity probe\n")}})

	after, err := e.Snapshot(context.Background(), dir, "")
	require.NoError(t, err)
	require.True(t, before.Diff(after).Empty())
}
