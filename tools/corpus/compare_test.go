package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A bump and its replay agree where their version, checksums, and
// dependency blocks hold the same words, whatever their order, and the
// rest of the Portfile is no part of it; a word either lacks is said.
func TestAReplayIsComparedAsWords(t *testing.T) {
	t.Parallel()
	bump := []byte(`PortSystem 1.0
PortGroup golang 1.0
go.setup github.com/owner/tool 1.1 v
revision 0
checksums tool-1.1.tar.gz \
    rmd160 aaaa \
    sha256 bbbb \
    size 10
go.vendors example.com/a lock v1.0.0 sha256 cccc size 1 \
           example.com/b lock v2.0.0 sha256 dddd size 2
notes "the person's own"
`)
	reordered := []byte(`PortSystem 1.0
PortGroup golang 1.0
go.setup github.com/owner/tool 1.1 v
revision 0
checksums tool-1.1.tar.gz \
    rmd160 aaaa \
    sha256 bbbb \
    size 10
go.vendors example.com/b lock v2.0.0 sha256 dddd size 2 \
           example.com/a lock v1.0.0 sha256 cccc size 1
`)
	want, err := read(bump)
	require.NoError(t, err)
	got, err := read(reordered)
	require.NoError(t, err)
	require.Empty(t, differences(want, got), "rows in another order, and notes apart")
	stale := []byte(`PortSystem 1.0
PortGroup golang 1.0
go.setup github.com/owner/tool 1.1 v
revision 0
checksums tool-1.1.tar.gz \
    rmd160 aaaa \
    sha256 bbbb \
    size 10
go.vendors example.com/a lock v1.0.0 sha256 cccc size 1 \
           example.com/b lock v1.9.0 sha256 eeee size 2
`)
	got, err = read(stale)
	require.NoError(t, err)
	require.Equal(t, []string{"go.vendors: the bump has dddd v2.0.0; the replay has eeee v1.9.0"}, differences(want, got))
}
