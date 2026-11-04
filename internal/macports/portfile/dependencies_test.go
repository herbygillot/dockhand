package portfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A port: dependency's word is rewritten in place, nothing else on its
// line moved; one whose word isn't there is left.
func TestADependencyIsRewrittenInPlace(t *testing.T) {
	t.Parallel()
	src := []byte("depends_build  port:pkgconfig port:cmake\nvariant x description {x} {\n    depends_lib-append port:pkgconfig\n}\n")
	dependencies := PortDependencies(src)
	require.Len(t, dependencies, 3)
	require.Equal(t, PortDependency{Line: 3, Option: "depends_lib-append", Port: "pkgconfig", Start: dependencies[2].Start, End: dependencies[2].End}, dependencies[2])
	rewritten := RewriteDependency(src, dependencies[2], "path:bin/pkg-config:pkgconfig")
	rewritten = RewriteDependency(rewritten, dependencies[0], "path:bin/pkg-config:pkgconfig")
	require.Equal(t, "depends_build  path:bin/pkg-config:pkgconfig port:cmake\nvariant x description {x} {\n    depends_lib-append path:bin/pkg-config:pkgconfig\n}\n", string(rewritten))
	require.Equal(t, src, RewriteDependency(src, PortDependency{Port: "zlib", Start: 15, End: 29}, "path:lib/libz.dylib:zlib"), "the word there isn't port:zlib")
}
