package portfile_test

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/stretchr/testify/require"
)

func TestRewriteLiteralDeclarationReplacesTheOneCarrier(t *testing.T) {
	t.Parallel()
	src := []byte("go.toolchain_min 1.22\nsubport x { go.toolchain_min ${v} }\n")
	out, err := portfile.RewriteLiteralDeclaration(src, "go.toolchain_min", "1.22", "1.24")
	require.NoError(t, err)
	require.Equal(t, "go.toolchain_min 1.24\nsubport x { go.toolchain_min ${v} }\n", string(out), "only the literal carrier changes")
	_, err = portfile.RewriteLiteralDeclaration(src, "go.toolchain_min", "1.23", "1.24")
	require.ErrorIs(t, err, portfile.ErrUnsupported, "a value not carried by one literal is refused")
	_, err = portfile.RewriteLiteralDeclaration([]byte("go.toolchain_min 1.22\ngo.toolchain_min 1.22\n"), "go.toolchain_min", "1.22", "1.24")
	require.ErrorIs(t, err, portfile.ErrUnsupported, "two carriers are refused too")
}
