package portedit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A PortGroup's checksum declaration moves with the version where a group
// it declares names an archive with the version in it, or names none,
// being the port's own archive; a fixed archive's digest that happens to
// contain the version isn't such a name (#337).
func TestAPortGroupChecksumDeclarationMovesTheVersionByItsGroupsNames(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		values []string
		moves  bool
	}{
		{[]string{"boot-1.2.tar.gz", "rmd160", "9aaa", "sha256", "bbbb", "size", "3"}, false},
		{[]string{"boot-1.2.tar.gz", "sha256", "9bbb", "size", "3", "app-9.tar.gz", "sha256", "cccc", "size", "4"}, true},
		{[]string{"rmd160", "aaaa", "sha256", "bbbb", "size", "3"}, true},
		{[]string{"boot-1.2.tar.gz rmd160 9aaa sha256 bbbb size 3"}, false},
	} {
		require.Equal(t, c.moves, checksumDeclarationMovesVersion(c.values, "9"), "%q", c.values)
	}
}
