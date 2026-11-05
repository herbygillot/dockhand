package portfile

import (
	"strings"
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

// An option's top words are its literal words and its -append's, at the
// top alone.
func TestAnOptionsTopWords(t *testing.T) {
	t.Parallel()
	src := []byte("license_noconflict openssl3 ${other}\nlicense_noconflict-append zlib\nvariant x description {x} {\n    license_noconflict-append bzip2\n}\n")
	require.Equal(t, []string{"openssl3", "zlib"}, TopWords(src, "license_noconflict"))
	require.Empty(t, TopWords([]byte("license MIT\n"), "license_noconflict"))
}

// tidy's declaration of what a check found linked goes after the
// Portfile's last depends_lib, aligned as it is, else its last depends_,
// else its checksums, set off by a blank line; and a change that only
// adds such a line is told apart from any other (#49).
func TestADeclarationIsWrittenAndToldApart(t *testing.T) {
	t.Parallel()
	before := []byte("name tool\nversion 1\n\ndepends_build       port:pkgconfig\ndepends_lib         port:zlib\n\nlivecheck.type      none\n")
	after, ok := DeclareLibrary(before, "port:libiconv")
	require.True(t, ok)
	require.Equal(t, "name tool\nversion 1\n\ndepends_build       port:pkgconfig\ndepends_lib         port:zlib\ndepends_lib-append  port:libiconv\n\nlivecheck.type      none\n", string(after))
	ports, ok := DeclaredChanges(before, after)
	require.True(t, ok)
	require.Equal(t, []string{"libiconv"}, ports)

	build, ok := DeclareLibrary([]byte("name tool\ndepends_build port:cmake\n"), "path:lib/libssl.dylib:openssl")
	require.True(t, ok)
	require.Equal(t, "name tool\ndepends_build port:cmake\ndepends_lib-append path:lib/libssl.dylib:openssl\n", string(build))
	sums, ok := DeclareLibrary([]byte("name tool\nchecksums           sha256  0\n"), "port:libiconv")
	require.True(t, ok)
	require.Equal(t, "name tool\nchecksums           sha256  0\n\ndepends_lib-append  port:libiconv\n", string(sums))
	_, ok = DeclareLibrary([]byte("name tool\nversion 1\n"), "port:libiconv")
	require.False(t, ok, "nowhere to put it")

	for _, other := range []string{
		strings.Replace(string(after), "version 1", "version 2", 1),
		strings.Replace(string(after), "depends_lib         port:zlib\n", "", 1),
	} {
		_, ok := DeclaredChanges(before, []byte(other))
		require.False(t, ok, other)
	}
}
