package eval

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The strip level is a number read out of an option's text, and every
// text the reader cannot make a number of is base's own default. The
// two spellings patch(1) accepts are both read; what it would refuse,
// or what names no level at all, is 0 rather than an error, because a
// planner asking "how many components does the patch phase discard"
// wants the answer the phase would act on, and for a port that said
// nothing that answer is -p0.
func TestStripLevel(t *testing.T) {
	for _, tc := range []struct {
		name string
		pre  string
		want int
	}{
		{"absent is base's default", "", 0},
		{"base's default, spelled out", DefaultPatchPreArgs, 0},
		{"-p0", "-p0", 0},
		{"-p1", "-p1", 1},
		{"-p 1, the separated spelling", "-p 1", 1},
		{"garbage", "garbage", 0},
		{"-p with nothing after it", "-p", 0},
		{"-p with a word after it", "-p one", 0},
		{"a level among other arguments", "--binary -p1", 1},
		{"the last -p wins, as it does for patch(1)", "-p0 -p2", 2},
		{"a negative level is not a level", "-p-1", 0},
		{"surrounding whitespace", "  -p1\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, StripLevel(tc.pre))
		})
	}
}

// A reply that never mentions patch.pre_args describes a port that
// patches at base's default, and the decoder says so in the option's
// own words rather than leaving a blank a reader would have to know
// the default to fill.
func TestDecodeDefaultsPatchPreArgs(t *testing.T) {
	v, _, err := decodeSnapshot("name jq version 1.7")
	require.NoError(t, err)
	assert.Equal(t, DefaultPatchPreArgs, v.PatchPreArgs)
	assert.Equal(t, 0, StripLevel(v.PatchPreArgs))

	v, _, err = decodeSnapshot("name jq version 1.7 patch.pre_args -p1")
	require.NoError(t, err)
	assert.Equal(t, "-p1", v.PatchPreArgs)
	assert.Equal(t, 1, StripLevel(v.PatchPreArgs))

	// A value with a space in it arrives braced, the way every other
	// configuration value does, and comes out as the option's text.
	v, _, err = decodeSnapshot("name jq version 1.7 patch.pre_args {--binary -p1}")
	require.NoError(t, err)
	assert.Equal(t, "--binary -p1", v.PatchPreArgs)
	assert.Equal(t, 1, StripLevel(v.PatchPreArgs))
}
