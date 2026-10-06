package depblock

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A Go module the Portfile keeps that go2port leaves out, as a test-only
// one, is told apart from any other difference, kept as declared where
// the new go.sum still pins it (field testing, batch 12: macpine's
// c2sp.org/CCTV/age and gopkg.in/check.v1).
func TestAGoModuleKeptByHandIsKept(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 64)
	other := strings.Repeat("b", 64)
	declared := []string{
		"c2sp.org/CCTV/age", "lock", "v0.0.0-20240306222714-3ec4d716e805", "sha256", sha, "size", "10",
		"github.com/x/y", "lock", "v1.0.0", "sha256", other, "size", "20",
	}
	generated := []string{"github.com/x/y", "lock", "v1.0.0", "sha256", other, "size", "20"}
	differences, err := Differences(Go, declared, generated)
	require.NoError(t, err)
	require.Equal(t, []Difference{{Name: "c2sp.org/CCTV/age", Declared: "v0.0.0-20240306222714-3ec4d716e805", Kept: true}}, differences)

	next := []string{"github.com/x/y", "lock", "v1.1.0", "sha256", other, "size", "21"}
	kept, err := KeepGoModules(declared, next, []string{"c2sp.org/CCTV/age"})
	require.NoError(t, err)
	require.Equal(t, append([]string{"c2sp.org/CCTV/age", "lock", "v0.0.0-20240306222714-3ec4d716e805", "sha256", sha, "size", "10"}, next...), kept, "in module order, as declared")

	// A kept module the new output has is its row, not a second one.
	withAge := append([]string{"c2sp.org/CCTV/age", "lock", "v0.0.0-20240306222714-3ec4d716e805", "sha256", other, "size", "11"}, next...)
	kept, err = KeepGoModules(declared, withAge, []string{"c2sp.org/CCTV/age"})
	require.NoError(t, err)
	require.Equal(t, withAge, kept)
	modules, err := GoModules(withAge)
	require.NoError(t, err)
	require.Equal(t, []string{"c2sp.org/CCTV/age", "github.com/x/y"}, modules)

	gosum := []byte("c2sp.org/CCTV/age v0.0.0-20240306222714-3ec4d716e805 h1:xyz=\ngithub.com/x/y v1.1.0/go.mod h1:abc=\n")
	require.True(t, GoSumPins(gosum, "c2sp.org/CCTV/age", "v0.0.0-20240306222714-3ec4d716e805"))
	require.True(t, GoSumPins(gosum, "github.com/x/y", "v1.1.0"), "by its go.mod's line")
	require.False(t, GoSumPins(gosum, "gopkg.in/check.v1", "v1.0.0-20201130134442-10cb98267c6c"))
}
