package eval

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGlobalsReportsWhatTheInterpreterHolds(t *testing.T) {
	e := newEvaluator(t)
	dir := portdirWith(t, `PortSystem 1.0
name globalprobe
set patchNumber 7
version 1.2.${patchNumber}
checksums rmd160 0 sha256 0 size 0
`)
	g, err := e.Globals(context.Background(), dir, "", "")
	require.NoError(t, err)
	assert.Equal(t, "1.2.7", g["version"])
	assert.Equal(t, "7", g["patchNumber"])
	assert.Equal(t, "globalprobe", g["name"])
}

// The carrier need not appear in the Portfile's text at all: a value
// composed from the subport's own name is an ordinary global here,
// which is what makes "the carrier is elsewhere" a statement rather
// than a shrug.
func TestGlobalsSeesACarrierTheTextDoesNotHold(t *testing.T) {
	e := newEvaluator(t)
	dir := portdirWith(t, `PortSystem 1.0
name subportprobe
version 0
checksums rmd160 0 sha256 0 size 0
subport subportprobe-2.9 {
    set baseVersion [lindex [split ${subport} "-"] 1]
    version ${baseVersion}.4
}
`)
	g, err := e.Globals(context.Background(), dir, "subportprobe-2.9", "")
	require.NoError(t, err)
	assert.Equal(t, "2.9.4", g["version"])
	assert.Equal(t, "2.9", g["baseVersion"], "the interpreter holds it though no literal in the file does")
}

// Arrays are namespaced machinery, not options, and prose is not a
// version fragment; neither belongs in the answer.
func TestGlobalsOmitsArraysAndLongValues(t *testing.T) {
	e := newEvaluator(t)
	dir := portdirWith(t, `PortSystem 1.0
name omitprobe
version 1.0
long_description This description is deliberately far longer than any version \
                 fragment could ever be, so that it is left out of the reply.
checksums rmd160 0 sha256 0 size 0
`)
	g, err := e.Globals(context.Background(), dir, "", "")
	require.NoError(t, err)
	assert.NotContains(t, g, "long_description")
	assert.NotContains(t, g, "PortInfo")
	assert.Equal(t, "1.0", g["version"])
}
