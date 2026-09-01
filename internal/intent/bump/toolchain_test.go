package bump

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tcl/syntax"
)

func parseFor(t *testing.T, src string) *syntax.Script {
	t.Helper()
	cst, errs := syntax.Parse([]byte(src))
	require.Empty(t, errs)
	return cst
}

func TestLocateToolchainMinFindsTheLiteral(t *testing.T) {
	src := "PortGroup golang 1.0\ngo.toolchain_min    1.22\n"
	span, v, ok := locateToolchainMin([]byte(src), parseFor(t, src), "x")
	require.True(t, ok)
	assert.Equal(t, "1.22", v)
	assert.Equal(t, "1.22", span.Text([]byte(src)))
}

func TestLocateToolchainMinSkipsComputedValues(t *testing.T) {
	src := "go.toolchain_min ${minver}\n"
	_, _, ok := locateToolchainMin([]byte(src), parseFor(t, src), "x")
	assert.False(t, ok, "update-only means never guessing at a computed declaration")
}

func TestGoSeriesComparesTheWayThePortGroupDoes(t *testing.T) {
	assert.Equal(t, "1.24", goSeries("1.24"))
	assert.Equal(t, "1.24", goSeries("1.24.3"))
	assert.NotEqual(t, goSeries("1.22"), goSeries("1.24.0"))
	// The no-churn rule: 1.22 and 1.22.0 are the same series.
	assert.Equal(t, goSeries("1.22"), goSeries("1.22.0"))
}

func TestGoDirectiveParsing(t *testing.T) {
	mod := "module example.com/x\n\ngo 1.24.0\n\nrequire (\n\tfoo v1.0.0\n)\n"
	m := goDirectiveRE.FindSubmatch([]byte(mod))
	require.NotNil(t, m)
	assert.Equal(t, "1.24.0", string(m[1]))
	assert.Nil(t, goDirectiveRE.FindSubmatch([]byte("module x\n// go 1.21 in a comment\n")))
}

func TestModelineEditAddsOnlyWhenMissing(t *testing.T) {
	e, ok := modelineEdit([]byte("PortSystem 1.0\nname x\n"))
	require.True(t, ok)
	assert.Equal(t, 0, e.Start)
	assert.Equal(t, 0, e.End)
	assert.Equal(t, Modeline+"\n", e.New)

	for _, first := range []string{
		Modeline,
		"# -*- coding: utf-8; mode: tcl -*-",
		"# vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4",
		"# vi: set ts=4:",
	} {
		_, ok := modelineEdit([]byte(first + "\nPortSystem 1.0\n"))
		assert.False(t, ok, "an existing modeline is never second-guessed: %s", first)
	}
	// An ordinary leading comment is not a modeline.
	_, ok = modelineEdit([]byte("# This port is maintained upstream\nPortSystem 1.0\n"))
	assert.True(t, ok)
}
