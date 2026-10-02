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
