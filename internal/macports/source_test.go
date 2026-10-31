package macports

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDarwinFloor(t *testing.T) {
	for platforms, floor := range map[string]int{
		"darwin":                0,
		"{darwin >= 20}":        20,
		"{darwin > 19}":         20,
		"macosx {darwin >= 21}": 21,
		"{darwin any}":          0,
		"{darwin >= 18 < 25}":   18,
	} {
		require.Equal(t, floor, DarwinFloor(platforms), platforms)
	}
}

func TestBaseAtLeast(t *testing.T) {
	require.True(t, BaseAtLeast("2.12.7", "2.12.6"))
	require.True(t, BaseAtLeast("11.0", "10.15"))
	require.False(t, BaseAtLeast("10.15", "11.0"))
}

// portindex is found as port-tclsh is: on the PATH first, then under the
// default prefix, and nowhere else.
func TestPortindexIsFoundAsTclshIs(t *testing.T) {
	look := func(found ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			for _, path := range found {
				if name == path {
					return "/found/" + name, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	require.Equal(t, "/found/portindex", FindPortindex(look("portindex", "/opt/local/bin/portindex")))
	require.Equal(t, "/found//opt/local/bin/portindex", FindPortindex(look("/opt/local/bin/portindex")))
	require.Empty(t, FindPortindex(look("port-tclsh")))
	require.Equal(t, "/found/port-tclsh", FindTclsh(look("port-tclsh")))
}
