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

// A module go2port couldn't fetch is named, with why, where go2port wrote
// it a checksum of 0: terraform-ls's github.com/chigopher/pathlib, whose
// repository is gone (the corpus, 2026-10-23).
func TestAModuleGo2portCouldntFetchIsNamed(t *testing.T) {
	t.Parallel()
	_, err := goRows([]string{"github.com/chigopher/pathlib", "lock", "v0.15.0", "sha256", "0", "size", "0"})
	missing := new(MissingChecksum)
	require.ErrorAs(t, err, &missing)
	require.Equal(t, MissingChecksum{Module: "github.com/chigopher/pathlib", Lock: "v0.15.0"}, *missing)
	stderr := []byte("WARNING: Could not calculate checksums for package: github.com/chigopher/pathlib\nCould not retrieve tarball for github.com/chigopher/pathlib; HTTP status=404\nExpected at: https://github.com/chigopher/pathlib/tarball/v0.15.0\n")
	require.EqualError(t, unfetched(*missing, stderr), "dependency: go2port couldn't fetch github.com/chigopher/pathlib v0.15.0's archive (HTTP 404); its repository may be gone, so it wrote the module no checksum")
	require.EqualError(t, unfetched(*missing, nil), "dependency: go2port wrote github.com/chigopher/pathlib v0.15.0 no checksum")
}
