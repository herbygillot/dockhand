package macports_test

import (
	"slices"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/stretchr/testify/require"
)

// ValidName is what every verb asks before it goes looking for a port or a
// contribution, so a path segment must fail here rather than survive as a
// name and come back later as work that does not exist.
func TestValidNameRefusesPathSegments(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"bashunit", "rb33-mustache", "py313-idna", "wxWidgets-3.2", "gtk3"} {
		require.True(t, macports.ValidName(name), name)
	}
	for _, name := range []string{"", ".", "..", "devel/bashunit", "two words", "tab\tname"} {
		require.False(t, macports.ValidName(name), name)
	}
}

func TestPortNamesAreOrderedAsAPersonReadsThem(t *testing.T) {
	names := []string{"terraform-1.10", "terraform", "terraform-1.2", "terraform-1.16", "terraform-1.9", "terraform_select", "py310-a", "py39-a", "x01", "x1"}
	slices.SortFunc(names, macports.ComparePortNames)
	require.Equal(t, []string{"py39-a", "py310-a", "terraform", "terraform-1.2", "terraform-1.9", "terraform-1.10", "terraform-1.16", "terraform_select", "x01", "x1"}, names)
}

// A port is named after a project where its name, case and its group's
// prefix set aside, is the project's (the person's rule for a port
// rename, 2026-10-29).
func TestNamedAfter(t *testing.T) {
	require.True(t, macports.NamedAfter("gop", "gop"))
	require.True(t, macports.NamedAfter("Gop", "GOP"))
	require.True(t, macports.NamedAfter("py-requests", "requests"))
	require.True(t, macports.NamedAfter("php-redis", "redis"))
	require.False(t, macports.NamedAfter("goplus", "gop"), "goplus's repository was gop")
	require.False(t, macports.NamedAfter("py-requests", "py-requests-oauth"))
	require.False(t, macports.NamedAfter("gop", ""))
	require.Equal(t, "py-bar", macports.RenamedAfter("py-foo", "Bar"))
	require.Equal(t, "xgo", macports.RenamedAfter("goplus", "xgo"))
}
