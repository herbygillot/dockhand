package macports_test

import (
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
