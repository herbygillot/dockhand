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
		"1.0-final": Unknown, "1.0.0-stable": Unknown, "8_0": Stable, "1_2_3": Stable,
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

func TestCrossesMajor(t *testing.T) {
	for _, test := range []struct {
		from, to string
		crosses  bool
	}{
		{"0.14.0", "1.179.0", true},
		{"1.7.1", "1.8.1", false},
		{"v2.9", "v3.0", true},
		{"9", "10", true},
		{"2025.1", "2026.1", false},
		{"20250914", "20251001", false},
		{"1.0", "abc", false},
		{"01.2", "1.3", false},
	} {
		require.Equal(t, test.crosses, CrossesMajor(test.from, test.to), "%s → %s", test.from, test.to)
	}
}

// A port's series is the one its name ends in where its version begins with
// it, terragrunt-0.52's 0.52 and kubectl-1.28's 1.28, and none otherwise.
func TestSeries(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, version, series string }{
		{"terragrunt-0.52", "0.52.1", "0.52"},
		{"kubectl-1.28", "1.28.15", "1.28"},
		{"llvm-17", "17.0.6", "17"},
		{"terragrunt-0.52", "0.53.0", ""},
		{"terragrunt-0.52", "0.52", ""},
		{"py-fs2", "2.0", ""},
		{"terragrunt", "0.52.1", ""},
		{"go-1.x", "1.2.3", ""},
	} {
		require.Equal(t, c.series, Series(c.name, c.version), c.name+" at "+c.version)
	}
	require.True(t, InSeries("0.52", "0.52.2"))
	require.True(t, InSeries("0.52", "0.52"))
	require.False(t, InSeries("0.52", "0.520.1"))
	require.False(t, InSeries("0.52", "1.2.1"))
}

// A candidate newer only by a component written with a leading zero is an
// older scheme, aften's 0.07 over 0.0.8; a date's month isn't.
func TestOlderScheme(t *testing.T) {
	t.Parallel()
	require.True(t, OlderScheme("0.0.8", "0.07"))
	require.False(t, OlderScheme("2023.12", "2024.01"), "the year differs first")
	require.False(t, OlderScheme("1.2.3", "1.2.4"))
	require.False(t, OlderScheme("0.07", "0.08"), "the current version writes leading zeros too")
	require.False(t, OlderScheme("1.0", "1.0.1"))
}
