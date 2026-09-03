package intent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports/info"
)

func TestExamineOffersTheModelineOnlyWhenMissing(t *testing.T) {
	ex := Examine([]byte("PortSystem 1.0\nname x\n"), nil, nil, nil, info.Values{}, nil)
	require.Len(t, ex.Riders, 1)
	assert.Equal(t, RuleModeline, ex.Riders[0].Rule)
	assert.Equal(t, 0, ex.Riders[0].Edit.Start)
	assert.Equal(t, 0, ex.Riders[0].Edit.End)
	assert.Equal(t, Modeline+"\n", ex.Riders[0].Edit.New)
	assert.Equal(t, "modeline", ex.Riders[0].Edit.Reason)

	for _, first := range []string{
		Modeline,
		"# -*- coding: utf-8; mode: tcl -*-",
		"# vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4",
		"# vi: set ts=4:",
	} {
		ex := Examine([]byte(first+"\nPortSystem 1.0\n"), nil, nil, nil, info.Values{}, nil)
		assert.Empty(t, ex.Riders, "an existing modeline is never second-guessed: %s", first)
	}

	// An ordinary leading comment is not a modeline.
	ex = Examine([]byte("# This port is maintained upstream\nPortSystem 1.0\n"), nil, nil, nil, info.Values{}, nil)
	assert.Len(t, ex.Riders, 1)
}

// The rest of the shape is declared, not implemented. The assertion is
// here so that the day a rule starts producing one, the step that did
// it is the step that changed this line.
func TestExamineProducesNoCascadesOrFindingsYet(t *testing.T) {
	ex := Examine([]byte("PortSystem 1.0\n"), nil, nil, nil, info.Values{}, []string{"a-dependent"})
	assert.Empty(t, ex.Cascades)
	assert.Empty(t, ex.Findings)
}

// The rider's rule name is the word a note carries, so a reader who
// sees "modeline" in a change's riders can look the rule up.
func TestRuleNamesAreTheNotesOwnWords(t *testing.T) {
	assert.Equal(t, "modeline", string(RuleModeline))
}
