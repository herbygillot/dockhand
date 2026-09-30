package macports

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A dependency is read as Base reads it, the port last: mise's
// port:bin/cmake:cmake is cmake, as Base builds it, and the reserved form
// Base's own test port uses is admitted too. What Base's patterns refuse
// is refused, and so are "." and "..", which Base's patterns admit as a
// port's name.
func TestADependencyIsReadAsBaseReadsIt(t *testing.T) {
	t.Parallel()
	for spec, port := range map[string]string{
		"port:cmake":                      "cmake",
		"port:bin/cmake:cmake":            "cmake",
		"port:-i_want_b:dependencies-a":   "dependencies-a",
		"port:a:b:py313-c":                "py313-c",
		"bin:git:git":                     "git",
		"bin:/usr/bin/perl:perl5":         "perl5",
		"lib:libz.1:zlib":                 "zlib",
		"path:lib/pkgconfig/zlib.pc:zlib": "zlib",
		"path:${prefix}/bin/g++:gcc14":    "gcc14",
	} {
		dependency, err := ParseDependency("build", spec)
		require.NoError(t, err, spec)
		require.Equal(t, Dependency{Port: port, Phase: "build", Spec: spec}, dependency)
		require.Equal(t, !strings.HasPrefix(spec, "port:"), dependency.MetByFile(), "only lib:, bin:, and path: are met by a file: %s", spec)
	}
	for _, spec := range []string{"cmake", "port:", "port::cmake", "bin::git", "lib:libz", "file:x:y", "path:a:b:c", "port:a/b", "port:x:..", "port:cmake ", "bin:a b:c"} {
		_, err := ParseDependency("build", spec)
		require.ErrorContains(t, err, "invalid dependency", spec)
	}
}
