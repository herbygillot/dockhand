package version

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassify(t *testing.T) {
	for version, want := range map[string]Stability{
		"1.2.3": Stable, "2026-09-14": Stable, "20240101": Stable, "1.16.3-1": Stable, "0.154.0": Stable,
		"0.155.0-alpha.15": Prerelease, "3.0-beta2": Prerelease, "1.0.0-rc1": Prerelease, "1.0rc1": Prerelease,
		"1.0a1": Prerelease, "1.0b2": Prerelease, "2.0.0.dev3": Prerelease, "1.5-SNAPSHOT": Prerelease,
		"4.0-pre": Prerelease, "20260916-nightly": Prerelease, "1.0.0-preview.2": Prerelease, "1.0-canary.3": Prerelease,
		"1.0.2u": Unknown, "1.2.3p4": Unknown, "2024q1": Unknown, "v1.2.3": Stable, "v2.403.9": Stable, "vv1": Unknown, "1.2.3-r1": Unknown, "": Unknown,
		"1.0-final": Unknown, "1.0.0-stable": Unknown,
	} {
		require.Equal(t, want, Classify(version), version)
	}
}

func TestLeavesStable(t *testing.T) {
	require.True(t, LeavesStable("0.154.0", "0.155.0-alpha.15"))
	require.False(t, LeavesStable("0.155.0-alpha.14", "0.155.0-alpha.15"), "already outside stable")
	require.False(t, LeavesStable("0.154.0", "0.155.0"))
	require.False(t, LeavesStable("0.154.0", "1.0.2u"), "unknown spellings are not called prereleases")
	require.False(t, LeavesStable("1.0.2u", "1.0.3-rc1"), "an unknown current version is not known to be stable")
}

func TestValidateAndTagPattern(t *testing.T) {
	for _, bad := range []string{"", "-1", "1 2", "1\t2", "\x00"} {
		require.ErrorIs(t, Validate(bad), ErrInput, "%q", bad)
	}
	require.NoError(t, Validate("v2.56.0-rc1"))
	p := TagPattern{Prefix: "v"}
	require.Equal(t, "v1.2", p.Tag("1.2"))
	got, ok := p.Version("v1.2")
	require.True(t, ok)
	require.Equal(t, "1.2", got)
	_, ok = p.Version("1.2")
	require.False(t, ok)
	_, ok = p.Version("v")
	require.False(t, ok)
	require.True(t, p.Explicit("v1.2"))
	require.False(t, p.Explicit("1.2"))
	require.False(t, TagPattern{}.Explicit("1.2"))
}
