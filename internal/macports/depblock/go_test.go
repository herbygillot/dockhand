package depblock

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A module is its go.package but for Go's major-version suffix, /vN or
// gopkg.in's .vN: direnv's github.com/direnv/direnv/v2 refused as moved
// (field testing, 2026-10-23). A module at another path moved, as pomo's
// did to Codeberg.
func TestAModuleIsItsPackageButForItsMajorVersion(t *testing.T) {
	t.Parallel()
	for _, same := range [][2]string{
		{"github.com/direnv/direnv", "github.com/direnv/direnv"},
		{"github.com/direnv/direnv/v2", "github.com/direnv/direnv"},
		{"github.com/direnv/direnv/v2", "github.com/direnv/direnv/v2"},
		{"gopkg.in/yaml.v3", "gopkg.in/yaml"},
		{"gopkg.in/yaml.v3", "gopkg.in/yaml.v3"},
	} {
		require.True(t, samePackage(same[0], same[1]), "%s is %s", same[0], same[1])
	}
	for _, moved := range [][2]string{
		{"codeberg.org/kevinschoon/pomo", "github.com/kevinschoon/pomo"},
		{"github.com/direnv/direnv-ng/v2", "github.com/direnv/direnv"},
		{"github.com/direnv/direnv/v3", "github.com/direnv/direnv/v2"},
	} {
		require.False(t, samePackage(moved[0], moved[1]), "%s moved from %s", moved[0], moved[1])
	}
}
