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
	require.False(t, baseLookupBenign(t.TempDir(), []byte("distfiles ${build.cmd}"), d, map[string][]byte{}, newBaseCode(b.lib)))
	d.Frames[1].File = "/foreign/Portfile"
	_, ok = recognizeBaseLookup(d, newBaseCode(b.lib))
	require.False(t, ok)
}
