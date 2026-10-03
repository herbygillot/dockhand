package project

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// configure.ac's options, pkg-config modules, and libraries, as its macros
// name them (field testing, batch 11: dateutils).
func TestReadAutoconfNamesWhatTheBuildAsks(t *testing.T) {
	t.Parallel()
	facts := ReadAutoconf([]byte(`AC_INIT([dateutils], [0.4.12])
AC_ARG_ENABLE([fast-arith], [AS_HELP_STRING([--enable-fast-arith], [faster])])
AC_ARG_WITH(old-links, [use old links])
PKG_CHECK_MODULES([GLIB], [glib-2.0 >= 2.30 gio-2.0])
AC_CHECK_LIB([m], [floor])
AC_SEARCH_LIBS([clock_gettime], [rt posix4])
`))
	require.Equal(t, AutoconfFacts{Enables: []string{"fast-arith"}, Withs: []string{"old-links"}, Modules: []string{"gio-2.0", "glib-2.0"}, Libraries: []string{"m", "posix4", "rt"}}, facts)
}
