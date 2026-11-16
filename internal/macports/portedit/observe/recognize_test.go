package observe

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMachineReadRecognitionDoesNotAcceptItsValueFlow(t *testing.T) {
	b := newBaseFixture(t)
	d := b.lookup("make", "base", b.substitution("build.cmd", "[portbuild::build_getmaketype]"), b.getmaketype())
	options, ok := recognizeBaseLookup(d, newBaseCode(b.lib))
	require.True(t, ok)
	require.Equal(t, []string{"build.cmd"}, options)
	require.False(t, experimentExplains(nil, 0), "recognition is no evidence")
	d.Frames[1].File = "/foreign/Portfile"
	_, ok = recognizeBaseLookup(d, newBaseCode(b.lib))
	require.False(t, ok)
}
