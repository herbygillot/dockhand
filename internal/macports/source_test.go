package macports

import (
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
